package config_test

import (
	"io/ioutil"
	"log"
	"os"
	"path"
	"testing"

	"gitpct.epam.com/epmd-aepr/aos_updatemanager/config"
)

/*******************************************************************************
 * Private
 ******************************************************************************/

func createConfigFile() (err error) {
	configContent := `{
	"ServerUrl": "localhost:8090",
	"Cert": "crt.pem",
	"Key": "key.pem"
}`

	if err := ioutil.WriteFile(path.Join("tmp", "aos_updatemanager.cfg"), []byte(configContent), 0644); err != nil {
		return err
	}

	return nil
}

func setup() (err error) {
	if err := os.MkdirAll("tmp", 0755); err != nil {
		return err
	}

	if err = createConfigFile(); err != nil {
		return err
	}

	return nil
}

func cleanup() (err error) {
	if err := os.RemoveAll("tmp"); err != nil {
		return err
	}

	return nil
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	if err := setup(); err != nil {
		log.Fatalf("Error creating service images: %s", err)
	}

	ret := m.Run()

	if err := cleanup(); err != nil {
		log.Fatalf("Error cleaning up: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetCredentials(t *testing.T) {
	config, err := config.New("tmp/aos_updatemanager.cfg")
	if err != nil {
		t.Fatalf("Error opening config file: %s", err)
	}

	if config.ServerURL != "localhost:8090" {
		t.Errorf("Wrong ServerURL value: %s", config.ServerURL)
	}

	if config.Cert != "crt.pem" {
		t.Errorf("Wrong cert value: %s", config.Cert)
	}

	if config.Key != "key.pem" {
		t.Errorf("Wrong key value: %s", config.Key)
	}
}
