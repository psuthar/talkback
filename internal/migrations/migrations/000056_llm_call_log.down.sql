-- SCRUM-568 (Slice 5 of SCRUM-560): rollback llm_call_log table.

DROP INDEX IF EXISTS llm_call_log_decision_ts_desc;
DROP INDEX IF EXISTS llm_call_log_site_ts_desc;
DROP INDEX IF EXISTS llm_call_log_ts_desc;
DROP TABLE IF EXISTS llm_call_log;
