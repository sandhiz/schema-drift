# schema-drift

A fast, zero-dependency, single-binary Go CLI tool that compares the structural metadata (tables and columns) of two databases across **PostgreSQL**, **MySQL / MariaDB**, and **SQL Server**, normalizes cross-engine data types, and outputs both a color-coded terminal diff and a self-contained single-file `report.html`.

---

## Features

- **Cross-Engine Schema Diff**: Compare SQL Server vs. PostgreSQL, MySQL vs. PostgreSQL, or same-engine environments (`staging` vs. `production`).
- **Intelligent Type Normalization**: Automatically maps 20+ common cross-engine data types (e.g., `nvarchar(50)` ↔ `character varying(50)`, `bit` ↔ `boolean`, `tinyint(1)` ↔ `boolean`, `datetime2` ↔ `timestamp`, `uniqueidentifier` ↔ `uuid`, `varbinary(max)` ↔ `bytea`) while still catching true mismatches (`decimal(10,2)` vs. `decimal(12,2)`, `varchar(50)` vs. `varchar(100)`).
- **Local Interactive Web UI (`schema-drift serve`)**: Built-in browser interface for entering Source A and Target B credentials (Form or DSN mode), testing read-only connections, inspecting live diffs, and downloading standalone `report.html` — with zero credentials written to disk.
- **Self-Contained HTML Report (`--out report.html`)**: Single HTML file with zero external CDNs or scripts, featuring automatic Dark/Light theme (`prefers-color-scheme`), instant search (`/`), clickable KPI summary filters, side-by-side column matrix, and Markdown summary copy.
- **Zero User Data Read**: Only executes read-only `SELECT` queries against `information_schema.*` and `sys.*` catalog views.
- **CI/CD Ready**: Deterministic exit codes (`0` identical, `1` drift detected, `2` error), `--ignore` glob filtering, environment-variable DSNs, and strict `***` password masking across stdout, stderr, and HTML output.
- **Single Static Binary**: Built with pure-Go database drivers (`CGO_ENABLED=0`) for Linux, macOS, and Windows.

---

## Prerequisites & Setup

What you need to install depends on whether you are running a prebuilt release binary or building from source:

### Option A: Using a Prebuilt Binary (Recommended for End Users)
- **No runtime dependencies required.** You do **not** need Go, C/CGO libraries, ODBC drivers, or Node.js installed.
- Download the binary for your OS and architecture (`linux`, `darwin`/macOS, or `windows`; `amd64` or `arm64`) from GitHub Releases and place it on your `PATH`.

### Option B: Building from Source / Local Development
Install the following tools before cloning and building:

1. **Go `1.26` or newer** (`go 1.27+` recommended, matching `go.mod`):
   - Download from https://go.dev/dl/
   - Verify installation:
     ```bash
     go version
     ```
2. **Git** (to clone the repository or run `go install`):
   ```bash
   git --version
   ```
3. **Docker & Docker Compose** *(Optional)*:
   - Only needed if you want to spin up local PostgreSQL 16, MySQL 8.4, and SQL Server 2022 containers (`docker-compose.test.yml`) to test against live databases.

---

## Installation

### 1. Run Directly from Source (Development)

No build step is needed while developing locally:

```bash
go run .
```

### 2. Install Globally (`schema-drift` available from any folder)

Install the binary into Go's `bin` directory (`GOPATH/bin`, e.g. `%USERPROFILE%\go\bin` on Windows or `~/go/bin` on Linux/macOS):

```bash
go install .
```

Once installed, you can run `schema-drift` from any terminal directory without `.\` or `.exe`.

### 3. Build a Standalone Static Binary

```bash
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o schema-drift .
```

On Windows PowerShell:

```powershell
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o schema-drift.exe .
```

---

## How to Run

### Option 1: Interactive Web UI (Recommended for Manual Comparison)

Start the local Web UI server — your default browser opens automatically (`http://127.0.0.1:8080`) while live logs stream in the terminal:

**During local development (inside the project folder):**
```bash
go run .
```
*(Or `go run . serve -v` to also print executed read-only catalog SQL queries.)*

**After running `go install .` (from any directory):**
```bash
schema-drift
```
*(Or `schema-drift serve -v` / `.\schema-drift.exe` if running the local built binary.)*

**Windows Explorer double-click:**
Double-click [`start-ui.bat`](start-ui.bat) to open the console log window and launch the Web UI automatically.

By default, the Web UI listens on `http://127.0.0.1:8080`. Just like a Node.js / NestJS server, real-time colored execution logs stream directly to your terminal (and inside the collapsible **Go Server Console** drawer at the bottom of the Web UI):

```text
[schema-drift] 14:27:01  OK     [Server    ] Web UI listening on http://127.0.0.1:8080 (read-only metadata mode)
[schema-drift] 14:27:05  INFO   [Test      ] Connecting Source A [MySQL/MariaDB] mysql://127.0.0.1:3306/app_staging
[schema-drift] 14:27:05  OK     [Test      ] Source A connected (MySQL/MariaDB) · 14 table(s) visible (18ms)
[schema-drift] 14:27:12  INFO   [Diff      ] Comparing A=[MySQL/MariaDB] mysql://127.0.0.1:3306/app_staging vs B=[MySQL/MariaDB] mysql://127.0.0.1:3306/app_prod
[schema-drift] 14:27:12  WARN   [Diff      ] Drift detected: onlyA=1, onlyB=0, modified=2, identical=11 (34ms)
```

Inside the UI you can:
1. Choose **MySQL**, **PostgreSQL**, or **SQL Server** for **Source A** and **Target B** (via structured Form fields or raw DSN paste).
2. Click **Test** to verify read-only catalog access and visible table count.
3. Click **Compare Schemas** (or **Sample Diff** to explore without live databases) to inspect side-by-side table/column diffs and export a standalone `report.html`.

### Option 2: Quick CLI Demo Preview (No Live Database Needed)

Want to preview the terminal diff and generate a sample static HTML report from the command line?

```bash
go run ./examples/demo --out docs/demo-report.html
```

Then open `docs/demo-report.html` in any web browser.

### Option 3: Compare Two Live Databases via CLI

#### PostgreSQL vs. PostgreSQL

```bash
schema-drift \
  --a "postgres://readonly:secret@staging-pg:5432/appdb?sslmode=require" \
  --b "postgres://readonly:secret@prod-pg:5432/appdb?sslmode=require"
```

#### Cross-Engine Migration (SQL Server -> PostgreSQL) + HTML Report

```bash
schema-drift \
  --a "sqlserver://readonly:secret@legacy-sql:1433?database=CoreBilling" \
  --b "postgres://readonly:secret@aurora-pg:5432/core_billing?sslmode=require" \
  --ignore "temp_*,audit_*" \
  --out report.html
```

#### MySQL / MariaDB vs. PostgreSQL

```bash
schema-drift \
  --a "mysql://readonly:secret@mysql.internal:3306/orders" \
  --b "postgres://readonly:secret@pg.internal:5432/orders" \
  --out report.html
```

*(Native `go-sql-driver/mysql` DSN format such as `readonly:secret@tcp(127.0.0.1:3306)/orders` is also supported.)*

### Recommended: Pass DSNs via Environment Variables

To keep database passwords out of shell history and OS process listings (`ps`), pass DSNs via `SCHEMA_DRIFT_A` and `SCHEMA_DRIFT_B`:

**Linux / macOS:**
```bash
export SCHEMA_DRIFT_A="sqlserver://readonly:secret@legacy-sql:1433?database=CoreBilling"
export SCHEMA_DRIFT_B="postgres://readonly:secret@aurora-pg:5432/core_billing"
schema-drift --ignore "temp_*,audit_*" --out report.html
```

**Windows PowerShell:**
```powershell
$env:SCHEMA_DRIFT_A = "sqlserver://readonly:secret@legacy-sql:1433?database=CoreBilling"
$env:SCHEMA_DRIFT_B = "postgres://readonly:secret@aurora-pg:5432/core_billing"
.\schema-drift.exe --ignore "temp_*,audit_*" --out report.html
```

### Exit Codes (CI/CD Integration)

| Exit Code | Meaning |
| --- | --- |
| `0` | Schemas are structurally identical (or `--help` / `--version` printed) |
| `1` | Schema drift detected (missing tables, missing columns, or column attribute diffs) |
| `2` | Invalid arguments, malformed glob/DSN, connection failure, or query/file error |

Example GitHub Actions step:

```yaml
- name: Check staging vs production schema drift
  env:
    SCHEMA_DRIFT_A: ${{ secrets.STAGING_READONLY_DSN }}
    SCHEMA_DRIFT_B: ${{ secrets.PROD_READONLY_DSN }}
  run: |
    ./schema-drift --ignore "temp_*,audit_*" --out schema-diff-report.html
```

---

## CLI Flags & Environment Variables

| Flag | Default | Description |
| --- | --- | --- |
| `--a` | `$SCHEMA_DRIFT_A` | Source database A DSN |
| `--b` | `$SCHEMA_DRIFT_B` | Target database B DSN |
| `--out` | `""` | Write self-contained HTML report to path (e.g., `report.html`) |
| `--ignore` | `""` | Comma-separated or repeatable glob patterns of tables to ignore (e.g., `"temp_*,audit_*"`) |
| `--schema-a` | `""` | Restrict Source A introspection to one schema |
| `--schema-b` | `""` | Restrict Target B introspection to one schema |
| `--label-a` | masked endpoint | Custom display label for Source A in outputs |
| `--label-b` | masked endpoint | Custom display label for Target B in outputs |
| `--strict-case` | `false` | Treat table/column casing differences (`Users` vs. `users`) as drift (exit `1`) |
| `--no-color` | `false` | Disable ANSI colors (also honored when `NO_COLOR` env var is non-empty) |
| `--timeout` | `30s` | Total timeout for database connections and metadata queries |
| `--version` | `false` | Print build version, commit, and date |

### Web UI Subcommand (`schema-drift serve`)

Running `schema-drift` (or `go run .`) with no arguments in an interactive terminal automatically starts `serve`.

| Flag | Default | Description |
| --- | --- | --- |
| `--addr` | `127.0.0.1:8080` | HTTP bind address and port for the local Web UI |
| `--open` | `true` | Automatically open the Web UI URL in your default browser |
| `--no-open` | `false` | Start the server without launching a browser window |
| `--verbose` / `-v` | `false` | Stream executed read-only catalog SQL queries (`[SQL]`) in logs |
| `--no-color` | `false` | Disable ANSI colors in server terminal logs |
| `--timeout` | `30s` | Default database connection and metadata introspection timeout |

---

## Web UI & Report Preview

![schema-drift Database Diff Studio — Dark Mode](docs/screenshots/webui-dark.png)

![schema-drift Active Diff Studio & Console — Dark Mode](docs/screenshots/webui-diff-dark.png)

![schema-drift HTML report — Light Mode](docs/screenshots/report-light.png)

![schema-drift HTML report — Dark Mode](docs/screenshots/report-dark.png)

---

## Cross-Engine Type Normalization

All type mappings live in a single canonical map ([`internal/normalizer/typemap.go`](internal/normalizer/typemap.go)):

| Canonical Type | SQL Server | PostgreSQL | MySQL / MariaDB | Parameters Compared |
| --- | --- | --- | --- | --- |
| `string` | `varchar`, `nvarchar`, `char`, `nchar` | `character varying`, `varchar`, `character`, `char`, `bpchar` | `varchar`, `char` | Character length |
| `text` | `text`, `ntext`, `varchar(max)`, `nvarchar(max)` | `text`, unbounded `varchar` | `text`, `tinytext`, `mediumtext`, `longtext` | — |
| `tinyint` | `tinyint` | — | `tinyint` (when not `tinyint(1)`) | — |
| `smallint` | `smallint` | `smallint`, `int2`, `smallserial` | `smallint` | — |
| `integer` | `int`, `integer` | `integer`, `int`, `int4`, `serial` | `int`, `integer`, `mediumint` | — |
| `bigint` | `bigint` | `bigint`, `int8`, `bigserial` | `bigint` | — |
| `decimal` | `decimal`, `numeric`, `money`, `smallmoney` | `numeric`, `decimal`, `money` | `decimal`, `numeric` | Precision & scale |
| `float32` | `real` | `real`, `float4` | `float` | — |
| `float64` | `float` | `double precision`, `float8` | `double`, `double precision` | — |
| `boolean` | `bit` | `boolean`, `bool` | `boolean`, `bool`, `tinyint(1)`, `bit(1)` | — |
| `date` | `date` | `date` | `date` | — |
| `time` | `time` | `time`, `time without time zone` | `time` | — |
| `timetz` | — | `time with time zone`, `timetz` | — | — |
| `timestamp` | `datetime`, `datetime2`, `smalldatetime` | `timestamp`, `timestamp without time zone` | `datetime` | — |
| `timestamptz` | `datetimeoffset` | `timestamp with time zone`, `timestamptz` | `timestamp` | — |
| `binary` | `binary`, `varbinary(n)` | — | `binary`, `varbinary` | Byte length |
| `blob` | `image`, `varbinary(max)` | `bytea` | `blob`, `tinyblob`, `mediumblob`, `longblob` | — |
| `uuid` | `uniqueidentifier` | `uuid` | `uuid` | — |
| `json` | — | `json`, `jsonb` | `json` | — |
| `xml` | `xml` | `xml` | — | — |

### Behavioral Notes
- **Unmapped Types**: Engine-specific types outside the canonical map (such as `hierarchyid`, `tsvector`, `enum`, `USER-DEFINED`, or `ARRAY`) are labeled `unmapped(<type>)` and reported without aborting execution.
- **Cross-Engine Defaults**: Because default expressions differ lexically across engines (`(getdate())` vs. `now()`), cross-engine diffs compare **presence vs. absence** of defaults. Same-engine diffs compare the normalized default expression directly.
- **Case Sensitivity**: Object names match case-insensitively by default, with casing differences flagged as informational notices unless `--strict-case` is set.

---

## Read-Only Database Permissions

Always use a dedicated **read-only user** with metadata-only access. Note that insufficient catalog privileges can cause tables to be invisible to `information_schema` or `sys.tables`; check the `tables read` count in the report header to confirm visibility.

### PostgreSQL
```sql
CREATE ROLE schema_drift_ro WITH LOGIN PASSWORD 'strong_password';
GRANT USAGE ON SCHEMA public TO schema_drift_ro;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO schema_drift_ro; -- or grant catalog visibility per schema
```
*(In PostgreSQL, `information_schema.tables` and `information_schema.columns` expose tables on which the role has any privilege or ownership.)*

### MySQL / MariaDB
```sql
CREATE USER 'schema_drift_ro'@'%' IDENTIFIED BY 'strong_password';
GRANT SELECT ON your_database.* TO 'schema_drift_ro'@'%';
```

### SQL Server
```sql
CREATE LOGIN schema_drift_ro WITH PASSWORD = 'Strong_Password123!';
USE YourDatabase;
CREATE USER schema_drift_ro FOR LOGIN schema_drift_ro;
GRANT VIEW DEFINITION TO schema_drift_ro;
```
*(SQL Server `VIEW DEFINITION` allows reading `sys.tables`, `sys.columns`, and `sys.default_constraints` without granting `SELECT` on user table rows.)*

---

## Security & Privacy Notice

- **Read-Only Guarantee**: `schema-drift` never executes `INSERT`, `UPDATE`, `DELETE`, `ALTER`, or `SELECT` against user tables.
- **Credential Redaction**: Passwords in `--a` / `--b` DSNs and driver errors are masked as `***` across terminal output, error messages, and `report.html`.
- **No Telemetry**: Zero outbound network requests are made other than to the two databases you specify.
- **Sharing `report.html`**: While `report.html` contains **zero row data**, it does include your **table names, column names, types, and masked hostnames**. Review before sharing outside your organization.

---

## Development & Testing

```bash
# Static analysis & all unit + 40 fixture tests
go vet ./...
go test -v ./...

# Verify >= 80% coverage on normalizer and diff packages
go test -cover ./internal/normalizer ./internal/diff

# Benchmark 500 tables / 5,000 columns per side
go test -bench=BenchmarkDiff500Tables5000Columns -benchmem ./internal/diff
```

### Optional Live Multi-Engine Integration Test

```bash
docker compose -f docker-compose.test.yml up -d
# Once containers are healthy:
TEST_PG_DSN="postgres://drift:drift_password@localhost:15432/app_target?sslmode=disable" \
TEST_MYSQL_DSN="mysql://drift:drift_password@localhost:13306/app_mysql" \
go test -tags=integration -v ./internal/introspect/...
```

---

## License

MIT License — see [LICENSE](LICENSE).
