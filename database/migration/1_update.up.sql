CREATE TABLE config_new (updateState TEXT);

INSERT INTO config_new (updateState)
SELECT updateState
FROM config;

DROP TABLE config;

ALTER TABLE config_new RENAME TO config;

DROP TABLE modules_data;
DROP TABLE certificates;

CREATE TABLE modules_new (id TEXT NOT NULL PRIMARY KEY,
						  vendorVersion TEXT,
			              aosVersion INTEGER,
			              state TEXT);

INSERT INTO modules_new (id, state)
SELECT id, state
FROM modules;

DROP TABLE modules;

ALTER TABLE modules_new RENAME TO modules;