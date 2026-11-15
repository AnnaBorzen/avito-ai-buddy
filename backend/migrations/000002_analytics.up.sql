-- Analytics facts from docs/metrics.md. Historical snapshots intentionally have
-- no FK to operations: events must survive operational data cleanup.
CREATE TABLE analytics_events (
    event_id         text        PRIMARY KEY,
    event            text        NOT NULL,
    occurred_at      timestamptz NOT NULL,
    actor_ref        text        NOT NULL,
    task_instance_id text        NOT NULL,
    scenario_id      text        NOT NULL,
    scenario_version text        NOT NULL,
    operation_id     text        NULL,
    outcome          text        NOT NULL,
    error_category   text        NULL,
    error_code       text        NULL,
    duration_ms      integer     NULL,
    model_version    text        NULL,
    test_mode        boolean     NOT NULL,
    source_ids       text[]      NULL,
    resource_id      text        NULL,
    access_level     text        NULL,
    draft_version    integer     NULL,
    ticket_id        text        NULL,
    recovered        boolean     NULL,
    reason           text        NULL,
    rating           integer     NULL,

    CONSTRAINT analytics_events_event_id_not_empty
        CHECK (event_id <> ''),
    CONSTRAINT analytics_events_event_valid
        CHECK (event IN (
            'task_started',
            'answer_delivered',
            'action_proposed',
            'action_confirmed',
            'action_succeeded',
            'action_failed',
            'action_status_unknown',
            'handoff_requested',
            'task_feedback_submitted'
        )),
    CONSTRAINT analytics_events_actor_ref_not_empty
        CHECK (actor_ref <> ''),
    CONSTRAINT analytics_events_task_instance_id_not_empty
        CHECK (task_instance_id <> ''),
    CONSTRAINT analytics_events_scenario_id_not_empty
        CHECK (scenario_id <> ''),
    CONSTRAINT analytics_events_scenario_version_not_empty
        CHECK (scenario_version <> ''),
    CONSTRAINT analytics_events_outcome_valid
        CHECK (outcome IN ('ok', 'denied', 'error', 'unknown')),
    CONSTRAINT analytics_events_error_category_valid
        CHECK (
            error_category IS NULL
            OR error_category IN (
                'rbac',
                'validation',
                'upstream_timeout',
                'upstream_error',
                'internal'
            )
        ),
    CONSTRAINT analytics_events_duration_ms_non_negative
        CHECK (duration_ms IS NULL OR duration_ms >= 0),
    CONSTRAINT analytics_events_draft_version_positive
        CHECK (draft_version IS NULL OR draft_version >= 1),
    CONSTRAINT analytics_events_access_level_valid
        CHECK (access_level IS NULL OR access_level IN ('read', 'write', 'admin')),
    CONSTRAINT analytics_events_reason_valid
        CHECK (reason IS NULL OR reason IN ('no_knowledge', 'user_initiated')),
    CONSTRAINT analytics_events_rating_non_negative
        CHECK (rating IS NULL OR rating >= 0),
    CONSTRAINT analytics_events_source_ids_no_nulls
        CHECK (source_ids IS NULL OR array_position(source_ids, NULL) IS NULL)
);

CREATE INDEX analytics_events_task_time_idx
    ON analytics_events (task_instance_id, occurred_at);

CREATE INDEX analytics_events_event_time_idx
    ON analytics_events (event, occurred_at);

CREATE INDEX analytics_events_operation_time_idx
    ON analytics_events (operation_id, occurred_at)
    WHERE operation_id IS NOT NULL;

CREATE INDEX analytics_events_actor_time_idx
    ON analytics_events (actor_ref, occurred_at);

-- A task has at most one start and one successful action. actor_ref is part of
-- the key because MVP task_instance_id values are not globally unique.
CREATE UNIQUE INDEX analytics_events_task_started_uq
    ON analytics_events (actor_ref, task_instance_id)
    WHERE event = 'task_started';

CREATE UNIQUE INDEX analytics_events_action_succeeded_uq
    ON analytics_events (actor_ref, task_instance_id)
    WHERE event = 'action_succeeded';
