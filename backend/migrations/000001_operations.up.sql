-- Operations, their immutable payload snapshot, access-service tickets and
-- idempotency records for confirm/reconcile. See docs/architecture/03-entities.md §9.
-- Migrations are applied by an explicit deploy step; the API never migrates on start.

CREATE SEQUENCE operation_id_seq AS integer START WITH 1;

CREATE TABLE operations (
    operation_id     text        PRIMARY KEY,
    user_id          text        NOT NULL,
    task_instance_id text        NOT NULL,
    scenario_id      text        NOT NULL,
    status           text        NOT NULL,
    draft_version    integer     NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT operations_operation_id_format
        CHECK (operation_id ~ '^op_[0-9]{1,10}$'),
    CONSTRAINT operations_user_id_not_empty
        CHECK (user_id <> ''),
    CONSTRAINT operations_task_instance_id_not_empty
        CHECK (task_instance_id <> ''),
    CONSTRAINT operations_scenario_id_not_empty
        CHECK (scenario_id <> ''),
    CONSTRAINT operations_status_valid
        CHECK (status IN (
            'draft',
            'awaiting_confirmation',
            'submitting',
            'succeeded',
            'failed',
            'unknown'
        )),
    CONSTRAINT operations_draft_version_positive
        CHECK (draft_version >= 1)
);

CREATE INDEX operations_user_created_idx
    ON operations (user_id, created_at DESC);

CREATE INDEX operations_user_status_idx
    ON operations (user_id, status);

CREATE INDEX operations_task_idx
    ON operations (task_instance_id);

CREATE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER operations_set_updated_at
    BEFORE UPDATE ON operations
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TABLE operation_payloads (
    operation_id   text PRIMARY KEY
        REFERENCES operations (operation_id) ON DELETE CASCADE,
    resource_id    text NOT NULL,
    resource_title text NOT NULL,
    access_level   text NOT NULL,
    justification  text NOT NULL,
    approver_id    text NOT NULL,
    approver_name  text NOT NULL,

    CONSTRAINT operation_payloads_resource_id_not_empty
        CHECK (resource_id <> ''),
    CONSTRAINT operation_payloads_resource_title_not_empty
        CHECK (resource_title <> ''),
    CONSTRAINT operation_payloads_access_level_requestable
        CHECK (access_level IN ('read', 'write')),
    -- The API counts characters (JSON Schema maxLength), not bytes.
    CONSTRAINT operation_payloads_justification_length
        CHECK (char_length(btrim(justification)) BETWEEN 10 AND 300),
    CONSTRAINT operation_payloads_approver_id_not_empty
        CHECK (approver_id <> ''),
    CONSTRAINT operation_payloads_approver_name_not_empty
        CHECK (approver_name <> '')
);

-- One operation has at most one access-service ticket (INV-01).
CREATE TABLE tickets (
    ticket_id       text        PRIMARY KEY,
    operation_id    text        NOT NULL UNIQUE
        REFERENCES operations (operation_id) ON DELETE RESTRICT,
    external_status text        NOT NULL,
    resource_id     text        NULL,
    access_level    text        NULL,
    approver_name   text        NULL,
    created_at      timestamptz NOT NULL,

    CONSTRAINT tickets_ticket_id_not_empty
        CHECK (ticket_id <> ''),
    CONSTRAINT tickets_external_status_valid
        CHECK (external_status IN ('pending', 'approved', 'rejected')),
    CONSTRAINT tickets_resource_id_not_empty
        CHECK (resource_id IS NULL OR resource_id <> ''),
    CONSTRAINT tickets_access_level_valid
        CHECK (access_level IS NULL OR access_level IN ('read', 'write', 'admin')),
    CONSTRAINT tickets_approver_name_not_empty
        CHECK (approver_name IS NULL OR approver_name <> '')
);

CREATE TABLE idempotency_keys (
    user_id         text        NOT NULL,
    operation_id    text        NOT NULL
        REFERENCES operations (operation_id) ON DELETE CASCADE,
    key             text        NOT NULL,
    request_hash    text        NOT NULL,
    response_status smallint    NULL,
    response_body   jsonb       NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),

    PRIMARY KEY (user_id, operation_id, key),

    CONSTRAINT idempotency_keys_user_id_not_empty
        CHECK (user_id <> ''),
    CONSTRAINT idempotency_keys_key_length
        CHECK (char_length(key) BETWEEN 8 AND 128),
    CONSTRAINT idempotency_keys_request_hash_not_empty
        CHECK (request_hash <> ''),
    CONSTRAINT idempotency_keys_response_status_range
        CHECK (response_status IS NULL OR response_status BETWEEN 100 AND 599),
    -- NULL response_status means the key is reserved but the external call is
    -- still in flight; a completed replay has both status and body.
    CONSTRAINT idempotency_keys_response_pair
        CHECK ((response_status IS NULL) = (response_body IS NULL))
);

CREATE INDEX idempotency_keys_created_idx
    ON idempotency_keys (created_at);

CREATE INDEX idempotency_keys_scope_idx
    ON idempotency_keys (user_id, operation_id);
