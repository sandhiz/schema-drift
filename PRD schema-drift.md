# PRD schema-drift

Nov 19, 2026 · @sandhi

schema-drift adalah satu binary Go yang membandingkan struktur dua database (SQL Server, PostgreSQL, MySQL/MariaDB) lalu menampilkan selisihnya di terminal dan di satu file laporan HTML. MVP ditargetkan selesai dalam 2–3 minggu.

## Ringkasan Eksekutif

**Problem statement.** Developer yang memigrasi atau menyamakan database antar environment atau antar engine (misalnya SQL Server lama ke PostgreSQL baru) harus mencari tabel, kolom, dan index yang berbeda secara manual, atau memakai tool berbayar dan tool yang hanya mendukung satu engine.

**Proposed solution.** Satu binary Go yang membaca metadata skema dari dua database, menormalisasi tipe data lintas engine, lalu melaporkan selisihnya sebagai output terminal berwarna dan satu file `report.html`.

**Success criteria** (target usulan, bisa diubah):

1. Waktu eksekusi ≤ 5 detik untuk membandingkan 500 tabel dan 5.000 kolom per sisi di jaringan lokal.
2. Nol baris data dibaca: hanya query ke `information_schema` atau `sys.*`, diverifikasi lewat log query di integration test.
3. Akurasi diff ≥ 95% pada test suite fixture berisi minimal 30 kasus (tabel hilang, kolom hilang, tipe beda, nullable beda, default beda, kasus lintas engine).
4. Test coverage ≥ 80% pada paket normalisasi tipe dan paket diff.
5. Satu binary tanpa dependency runtime, tersedia untuk Linux, macOS, dan Windows lewat GoReleaser.

## Pengalaman Pengguna & Fungsionalitas

### Persona

| Persona | Konteks | Kebutuhan utama |
| --- | --- | --- |
| Backend developer | Memigrasi database antar engine (misalnya SQL Server ke PostgreSQL) | Tahu tabel dan kolom apa yang belum terbawa |
| DevOps / developer | Menjaga staging dan production tetap sama | Mendeteksi drift skema sebelum deploy, termasuk di CI |
| Tech lead / reviewer | Memeriksa hasil migrasi tanpa akses langsung ke database | Laporan yang bisa dibuka dan dibagikan sebagai satu file |

### User story dan acceptance criteria

**US1.** Sebagai backend developer, saya ingin membandingkan dua database lewat DSN agar tahu tabel dan kolom yang hilang di salah satu sisi.

- `schema-drift --a <dsn> --b <dsn>` berjalan tanpa file konfigurasi.
- Output memuat tabel yang hanya ada di A, tabel yang hanya ada di B, dan kolom yang hanya ada di satu sisi untuk tiap tabel.
- Exit code 0 jika skema identik, 1 jika ada selisih, 2 jika koneksi atau argumen bermasalah.

**US2.** Sebagai developer yang migrasi lintas engine, saya ingin tipe data dinormalisasi agar `nvarchar(50)` vs `varchar(50)` tidak dilaporkan sebagai perbedaan palsu.

- Pemetaan tipe tersimpan di satu file dan mencakup minimal 15 pasangan tipe umum (string, integer, decimal, tanggal/waktu, boolean, binary).
- Selisih panjang, presisi, dan scale tetap dilaporkan (contoh: `decimal(10,2)` vs `decimal(12,2)`).
- Tipe yang tidak ada di pemetaan ditandai `unmapped` di laporan dan tidak membuat program berhenti.

**US3.** Sebagai tech lead, saya ingin laporan HTML satu file agar hasil diff bisa dibagikan tanpa akses ke database.

- `--out report.html` menghasilkan satu file tanpa CDN atau script eksternal.
- Bagian atas memuat ringkasan jumlah (tabel hilang, kolom hilang, tipe beda).
- Warna konsisten: hijau untuk hanya di A, merah untuk hanya di B, kuning untuk tipe atau atribut berbeda.
- Ada kolom pencarian untuk memfilter nama tabel, dan tampilan mengikuti `prefers-color-scheme` (dark mode).

**US4.** Sebagai DevOps, saya ingin mengabaikan tabel tertentu dan memakai tool ini di CI agar drift terdeteksi otomatis.

- `--ignore "temp_*,audit_*"` mengecualikan tabel yang cocok dengan pola glob.
- Exit code non-zero saat ada selisih sehingga pipeline CI gagal tanpa parsing output.
- Password di DSN tidak pernah dicetak di output, log, maupun laporan HTML.

### Non-goals (MVP)

- Tidak membandingkan isi baris (data diff).
- Tidak menjalankan `ALTER` atau menulis apa pun ke database; tool ini hanya membaca.
- Tidak ada mode `serve` dengan web UI; MVP hanya CLI dan file HTML statis.
- Tidak membandingkan index, PK/FK, view, stored procedure, dan trigger (dijadwalkan di v1.1 dan v2.0).
- Tidak mendukung engine selain SQL Server, PostgreSQL, dan MySQL/MariaDB.

## Spesifikasi Teknis

### Arsitektur dan alur data

1. **CLI** mem-parse flag (`--a`, `--b`, `--out`, `--ignore`) dan memvalidasi argumen.
2. **Introspector per engine** membuka koneksi read-only lalu menjalankan query metadata. Setiap engine mengimplementasikan interface yang sama sehingga engine baru cukup menambah satu paket.
3. **Normalizer** memetakan hasil ke model internal yang sama (`Table`, `Column`) dan mengubah tipe asli menjadi tipe kanonik (misalnya `nvarchar` dan `varchar` menjadi `string`).
4. **Diff engine** membandingkan dua model: tabel hanya di A atau B, kolom hanya di A atau B, dan atribut kolom yang berbeda (tipe kanonik, panjang/presisi/scale, nullable, default).
5. **Renderer** menulis output terminal berwarna dan, jika `--out` diberikan, file HTML dari `html/template` yang di-embed lewat `go:embed`.
6. **Exit code** ditentukan dari hasil diff: 0 identik, 1 ada selisih, 2 error.

```go
type Introspector interface {
    Tables(ctx context.Context) ([]Table, error)
}
```

### Integration points

| Komponen | Detail | Status |
| --- | --- | --- |
| SQL Server | Metadata dari `sys.tables`, `sys.columns`, `sys.types` | Di MVP |
| PostgreSQL | Metadata dari `information_schema.tables` dan `information_schema.columns` | Di MVP |
| MySQL/MariaDB | Metadata dari `information_schema.tables` dan `information_schema.columns` | Di MVP |
| Driver database Go | Dipilih saat implementasi; harus pure Go (tanpa CGO) agar tetap satu binary | TBD |
| Autentikasi | Tidak ada sistem auth sendiri; kredensial hanya lewat DSN | Di MVP |
| Build dan rilis | GitHub Actions untuk CI, GoReleaser untuk binary Linux, macOS, Windows | Di MVP |
| Versi Go dan lisensi repo | Belum ditentukan | TBD |

### Keamanan dan privasi

- Tool hanya menjalankan `SELECT` pada katalog metadata. README menyarankan memakai user database read-only.
- Password di DSN dimasking (`***`) di semua output, log, dan laporan HTML.
- DSN bisa dikirim lewat environment variable agar tidak terlihat di daftar proses; flag tetap didukung untuk kemudahan.
- Laporan HTML hanya berisi nama tabel, nama kolom, dan tipe; tidak ada data baris. Karena nama tabel dan kolom pun bisa sensitif, README memberi peringatan sebelum laporan dibagikan.
- Tidak ada telemetry dan tidak ada koneksi keluar selain ke dua database yang diberikan pengguna.

## Risiko & Roadmap

### Rollout bertahap

| Fase | Cakupan | Kriteria selesai | Waktu |
| --- | --- | --- | --- |
| MVP | Koneksi ke 3 engine; diff tabel dan kolom (hilang, tipe, nullable, default); normalisasi tipe; output terminal dan `report.html`; `--ignore`; exit code; CI dan GoReleaser | US1–US4 lulus; KPI 1–5 tercapai pada fixture; README berisi screenshot laporan | 2–3 minggu |
| v1.1 | Diff index, PK, FK, dan unique constraint; `--format json`; skrip `ALTER TABLE` yang hanya dicetak, tidak dijalankan | Acceptance criteria ditulis sebelum fase dimulai | TBD setelah MVP |
| v2.0 | Mode `serve` dengan web UI dua kolom (HTMX); diff view dan stored procedure; engine tambahan bila ada permintaan | Ditentukan setelah v1.1 | TBD |

### Risiko teknis

| Risiko | Dampak | Mitigasi |
| --- | --- | --- |
| Pemetaan tipe lintas engine tidak lengkap (misalnya `datetime2` vs `timestamp`, `bit` vs `boolean`, `money`) | Perbedaan palsu membuat laporan tidak dipercaya | Pemetaan di satu file, tipe tak dikenal ditandai `unmapped`, fixture test untuk tiap pasangan tipe |
| Ekspresi default berbeda antar engine (`getdate()` vs `now()`) | Banyak selisih palsu pada diff lintas engine | Lintas engine hanya membandingkan ada atau tidaknya default; string default dibandingkan hanya untuk engine yang sama |
| Huruf besar-kecil nama objek berbeda antar engine | Tabel yang sama terbaca sebagai hilang | Nama dibandingkan case-insensitive secara default dan perbedaan huruf ditandai di laporan |
| User database read-only tidak melihat semua objek katalog | Tabel tampak hilang padahal hanya tidak terlihat | Ringkasan laporan menampilkan jumlah tabel yang terbaca per sisi; README menjelaskan hak akses minimum |
| Driver pihak ketiga memakai CGO atau tidak lagi dirawat | Binary tidak lagi tunggal, atau build rusak | Pilih driver pure Go dan sembunyikan di balik interface `Introspector` |
| Scope creep (data diff, mode `serve`) | MVP melewati 2–3 minggu | Non-goals dijaga ketat; fitur tambahan masuk roadmap v1.1 dan v2.0 |
