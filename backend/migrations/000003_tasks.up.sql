-- Durable task state for the chat scenario. The backend owns task progress,
-- messages and partially collected slots; ML remains stateless.
CREATE SEQUENCE task_instance_seq AS integer START WITH 1;

CREATE TABLE tasks (
    task_instance_id text        PRIMARY KEY,
    user_id          text        NOT NULL UNIQUE,
    scenario_id      text        NOT NULL,
    rules_explained  boolean     NOT NULL DEFAULT false,
    resource_id      text        NULL,
    access_level     text        NULL,
    justification    text        NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT tasks_task_instance_id_format
        CHECK (task_instance_id ~ '^tsk_[0-9]{1,10}$'),
    CONSTRAINT tasks_user_id_not_empty
        CHECK (user_id <> ''),
    CONSTRAINT tasks_scenario_id_not_empty
        CHECK (scenario_id <> ''),
    CONSTRAINT tasks_access_level_valid
        CHECK (access_level IS NULL OR access_level IN ('read', 'write')),
    CONSTRAINT tasks_justification_length
        CHECK (justification IS NULL OR char_length(justification) <= 300)
);

CREATE TRIGGER tasks_set_updated_at
    BEFORE UPDATE ON tasks
    FOR EACH ROW
    EXECUTE FUNCTION set_updated_at();

CREATE TABLE task_messages (
    message_id       text        PRIMARY KEY,
    task_instance_id text        NOT NULL
        REFERENCES tasks (task_instance_id) ON DELETE CASCADE,
    role             text        NOT NULL,
    content          text        NOT NULL,
    source_doc_id    text        NULL,
    source_title     text        NULL,
    source_section   text        NULL,
    source_excerpt   text        NULL,
    source_synthetic boolean     NULL,
    handoff          boolean     NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT task_messages_role_valid
        CHECK (role IN ('user', 'assistant')),
    CONSTRAINT task_messages_content_not_empty
        CHECK (content <> ''),
    CONSTRAINT task_messages_source_complete
        CHECK (
            (source_doc_id IS NULL AND source_title IS NULL AND source_section IS NULL
                AND source_excerpt IS NULL AND source_synthetic IS NULL)
            OR
            (source_doc_id IS NOT NULL AND source_title IS NOT NULL AND source_section IS NOT NULL
                AND source_excerpt IS NOT NULL AND source_synthetic IS NOT NULL)
        )
);

CREATE INDEX task_messages_task_time_idx
    ON task_messages (task_instance_id, created_at);
