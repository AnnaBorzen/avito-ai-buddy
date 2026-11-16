-- Durable state of the separate access-service mock. It intentionally has no
-- foreign keys to backend operations: the service owns its records and only
-- exchanges tickets through contracts/access-service.openapi.yaml.
CREATE SEQUENCE access_ticket_seq AS integer START WITH 1;

CREATE TABLE access_service_tickets (
    operation_id    text        PRIMARY KEY,
    ticket_id       text        NOT NULL UNIQUE,
    user_id         text        NOT NULL,
    team_id         text        NOT NULL,
    resource_id     text        NOT NULL,
    access_level    text        NOT NULL,
    justification   text        NOT NULL,
    approver_id     text        NOT NULL,
    approver_name   text        NOT NULL,
    external_status text        NOT NULL DEFAULT 'pending',
    created_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT access_service_operation_id_format
        CHECK (operation_id ~ '^op_[0-9]{1,10}$'),
    CONSTRAINT access_service_ids_not_empty
        CHECK (ticket_id <> '' AND user_id <> '' AND team_id <> '' AND resource_id <> ''
            AND approver_id <> '' AND approver_name <> ''),
    CONSTRAINT access_service_access_level_valid
        CHECK (access_level IN ('read', 'write')),
    CONSTRAINT access_service_justification_length
        CHECK (char_length(btrim(justification)) BETWEEN 10 AND 300),
    CONSTRAINT access_service_status_valid
        CHECK (external_status IN ('pending', 'approved', 'rejected'))
);
