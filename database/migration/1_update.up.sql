CREATE TABLE config_new (updateState TEXT);

INSERT INTO config_new (updateState)
SELECT updateState
FROM config;

DROP TABLE config;

ALTER TABLE config_new RENAME TO config;
