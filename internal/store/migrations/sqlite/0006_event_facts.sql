-- P22 `wtc explain`: the redacted ingest-time rule facts (plus the pre-rules
-- field snapshot) an event was normalized from. Nullable — rows ingested
-- before this migration report "facts not recorded", never a guess.
ALTER TABLE events ADD COLUMN facts TEXT;
