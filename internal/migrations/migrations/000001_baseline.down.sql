-- Single baseline down: drop all objects in public schema (no data preservation).
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO public;
