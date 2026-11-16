-- Roll back the access-service mock state; it owns no backend foreign keys.
DROP TABLE IF EXISTS access_service_tickets;
DROP SEQUENCE IF EXISTS access_ticket_seq;
