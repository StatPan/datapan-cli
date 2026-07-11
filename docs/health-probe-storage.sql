-- PostgreSQL reference mapping for datapan.health-probe.v1 receipts.
-- This is a service-side storage contract; datapan-cli does not require a DB.

create table registry_revisions (
    id uuid primary key,
    dataset_id text not null,
    dataset_revision text not null check (dataset_revision ~ '^[0-9a-f]{40,64}$'),
    registry_sha256 char(64) not null,
    manifest_sha256 char(64),
    published_at timestamptz,
    unique (dataset_id, dataset_revision)
);

create table operation_versions (
    id uuid primary key,
    registry_revision_id uuid not null references registry_revisions(id),
    operation_key char(64) not null,
    dataset_id text not null,
    operation_name text not null,
    provider text not null,
    endpoint_host text,
    endpoint_path text,
    dependency_class text not null,
    contract_sha256 char(64),
    unique (registry_revision_id, operation_key)
);

create index operation_versions_lookup
    on operation_versions (operation_key, registry_revision_id);

create table probe_policies (
    id uuid primary key,
    policy_key text not null,
    version integer not null check (version > 0),
    operation_key char(64),
    max_level text not null check (max_level in ('L0','L1','L2','L3','L4','L5','L6','L7')),
    credential_class text,
    safe_parameters jsonb not null default '{}'::jsonb,
    success_assertions jsonb not null default '[]'::jsonb,
    shape_assertions jsonb not null default '[]'::jsonb,
    freshness_assertions jsonb not null default '[]'::jsonb,
    timeout_ms integer not null check (timeout_ms > 0),
    request_budget integer not null check (request_budget >= 0),
    created_at timestamptz not null default now(),
    unique (policy_key, version)
);

create table probe_runs (
    id uuid primary key,
    registry_revision_id uuid not null references registry_revisions(id),
    policy_id uuid references probe_policies(id),
    cli_version text not null,
    trigger text not null,
    started_at timestamptz not null,
    finished_at timestamptz,
    request_budget integer not null check (request_budget >= 0),
    workers integer not null check (workers > 0),
    timeout_ms integer not null check (timeout_ms > 0),
    receipt_schema_version text not null
);

create index probe_runs_started_at on probe_runs (started_at desc);

create table probe_results (
    id uuid primary key,
    run_id uuid not null references probe_runs(id),
    operation_version_id uuid not null references operation_versions(id),
    observed_at timestamptz not null,
    attempted boolean not null,
    max_observed_level text not null check (max_observed_level in ('L0','L1','L2','L3','L4','L5','L6','L7')),
    outcome text not null,
    category text not null,
    retryable boolean not null,
    latency_ms integer check (latency_ms >= 0),
    http_status integer check (http_status between 100 and 599),
    provider_code text,
    semantic_status text,
    body_shape text,
    data_presence text not null check (data_presence in ('not_observed','empty','present','indeterminate')),
    schema_status text not null check (schema_status in ('not_observed','conformant','drift','indeterminate')),
    freshness_status text not null check (freshness_status in ('not_observed','fresh','stale','indeterminate')),
    reason_code text,
    receipt jsonb not null,
    receipt_sha256 char(64) not null unique
);

create index probe_results_operation_time
    on probe_results (operation_version_id, observed_at desc);
create index probe_results_category_time
    on probe_results (category, observed_at desc);

-- A production service may range-partition probe_results by observed_at.
-- Reclassification belongs in versioned views/tables, never UPDATEs of receipt.
