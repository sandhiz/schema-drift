-- SQL Server 2022 reference seed (Source A / Cross-engine comparison)
CREATE TABLE dbo.Users (
    id BIGINT IDENTITY(1,1) PRIMARY KEY,
    email NVARCHAR(120) NOT NULL,
    full_name NVARCHAR(80) NOT NULL,
    is_active BIT NOT NULL DEFAULT ((1)),
    created_at DATETIMEOFFSET NOT NULL DEFAULT (SYSDATETIMEOFFSET()),
    org_node HIERARCHYID NULL
);

CREATE TABLE dbo.orders (
    id UNIQUEIDENTIFIER NOT NULL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    total_amount DECIMAL(10, 2) NOT NULL DEFAULT ((0.00)),
    status NVARCHAR(32) NOT NULL DEFAULT ('pending'),
    notes NVARCHAR(MAX) NULL
);

CREATE TABLE dbo.legacy_audit_dump (
    id INT PRIMARY KEY,
    payload NVARCHAR(MAX) NULL
);
