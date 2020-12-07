ALTER TABLE config ADD version INTEGER;
UPDATE config SET version = 5;
