ALTER TABLE config ADD version INTEGER;
UPDATE config SET version = 5;

CREATE TABLE modules_new (id TEXT NOT NULL PRIMARY KEY, state TEXT);

INSERT INTO modules_new (id, state)
SELECT id, state
FROM modules;

DROP TABLE modules;

ALTER TABLE modules_new RENAME TO modules;
