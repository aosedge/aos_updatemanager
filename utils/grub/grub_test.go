package grub_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/grub"
)

/*******************************************************************************
 * Init
 ******************************************************************************/

func init() {
	log.SetFormatter(&log.TextFormatter{
		DisableTimestamp: false,
		TimestampFormat:  "2006-01-02 15:04:05.000",
		FullTimestamp:    true})
	log.SetLevel(log.DebugLevel)
	log.SetOutput(os.Stdout)
}

/*******************************************************************************
 * Main
 ******************************************************************************/

func TestMain(m *testing.M) {
	var err error

	if err = os.MkdirAll("tmp", 0755); err != nil {
		log.Fatalf("Error creating tmp dir: %s", err)
	}

	if err = exec.Command("grub-editenv", "tmp/drub.env", "create").Run(); err != nil {
		log.Fatalf("Can't create env file: %s", err)
	}

	ret := m.Run()

	if err = os.RemoveAll("tmp"); err != nil {
		log.Fatalf("Error removing tmp dir: %s", err)
	}

	os.Exit(ret)
}

/*******************************************************************************
 * Tests
 ******************************************************************************/

func TestGetVariable(t *testing.T) {
	varName := `TEST_VAR1`
	varValue := `Test\value\1\\`

	if err := exec.Command("grub-editenv", "tmp/drub.env", "set", varName+"="+varValue).Run(); err != nil {
		log.Fatalf("Can't set env variable: %s", err)
	}

	grub, err := grub.New("tmp/drub.env")
	if err != nil {
		t.Fatalf("Can't create grub instance: %s", err)
	}
	defer grub.Close()

	getValue, err := grub.GetVariable(varName)
	if err != nil {
		t.Fatalf("Can't get variable: %s", err)
	}

	if getValue != varValue {
		t.Errorf("Wrong env var value: %s", getValue)
	}
}

func TestSetVariable(t *testing.T) {
	varName := `TEST_VAR2`
	varValue := `Var 2 value`

	grub, err := grub.New("tmp/drub.env")
	if err != nil {
		t.Fatalf("Can't create grub instance: %s", err)
	}
	defer grub.Close()

	if err := grub.SetVariable(varName, varValue); err != nil {
		t.Fatalf("Can't set variable: %s", err)
	}

	getValue, err := grub.GetVariable(varName)
	if err != nil {
		t.Fatalf("Can't get variable: %s", err)
	}

	if getValue != varValue {
		t.Errorf("Wrong env value: %s", getValue)
	}

	if err := grub.Store(); err != nil {
		t.Fatalf("Can't store grub env: %s", err)
	}

	info, err := os.Stat("tmp/drub.env")
	if err != nil {
		t.Fatalf("Can't get file info: %s", err)
	}

	if info.Size() != 1024 {
		t.Fatalf("Invalid file size: %d", info.Size())
	}

	output, err := exec.Command("grub-editenv", "tmp/drub.env", "list").CombinedOutput()
	if err != nil {
		log.Fatalf("Can't get env list: %s", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		data := strings.Split(line, "=")

		if data[0] == varName {
			if data[1] != varValue {
				log.Errorf("Wrong env value: %s", data[0])
			}
		}
	}
}

func TestUnsetVariable(t *testing.T) {
	varName := `TEST_VAR_UNSET`
	varValue := `Value of unset var`

	if err := exec.Command("grub-editenv", "tmp/drub.env", "set", varName+"="+varValue).Run(); err != nil {
		log.Fatalf("Can't set env variable: %s", err)
	}

	grub, err := grub.New("tmp/drub.env")
	if err != nil {
		t.Fatalf("Can't create grub instance: %s", err)
	}
	defer grub.Close()

	if err := grub.UnsetVariable(varName); err != nil {
		t.Fatalf("Can't set variable: %s", err)
	}

	if _, err = grub.GetVariable(varName); err == nil {
		t.Fatalf("Should return error: %s", err)
	}

	if err := grub.Store(); err != nil {
		t.Fatalf("Can't store grub env: %s", err)
	}

	info, err := os.Stat("tmp/drub.env")
	if err != nil {
		t.Fatalf("Can't get file info: %s", err)
	}

	if info.Size() != 1024 {
		t.Fatalf("Invalid file size: %d", info.Size())
	}

	output, err := exec.Command("grub-editenv", "tmp/drub.env", "list").CombinedOutput()
	if err != nil {
		log.Fatalf("Can't get env list: %s", err)
	}

	for _, line := range strings.Split(string(output), "\n") {
		data := strings.Split(line, "=")

		if data[0] == varName {
			log.Errorf("Variable should not exist: %s", data[0])
		}
	}
}
