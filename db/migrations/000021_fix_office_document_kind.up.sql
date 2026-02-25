-- Fix materials that were stored with kind 'other' but are Office Word documents.
-- Participant mode only shows items with kind 'document' under DOCUMENTS.
UPDATE materials
SET kind = 'document'
WHERE kind = 'other'
  AND (
    content_type LIKE '%wordprocessingml%'
    OR LOWER(filename) LIKE '%.docx'
    OR LOWER(filename) LIKE '%.doc'
  );
