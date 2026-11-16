-- Roll back durable task state. Messages reference tasks, so they go first.
DROP TABLE IF EXISTS task_messages;
DROP TABLE IF EXISTS tasks;
DROP SEQUENCE IF EXISTS task_instance_seq;
