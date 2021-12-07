CREATE TABLE modules_new (
	id TEXT NOT NULL PRIMARY KEY,
	aosVersion INTEGER,
	state TEXT);

INSERT INTO modules_new (id, aosVersion, state) SELECT id, aosVersion, state FROM modules;

DROP TABLE modules;

ALTER TABLE modules_new RENAME TO modules;