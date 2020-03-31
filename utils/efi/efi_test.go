package efi_test

import (
	"errors"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"

	"aos_updatemanager/utils/efi"
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
 * Tests
 ******************************************************************************/

func TestGetByPartUUID(t *testing.T) {
	efiVars, err := efi.New()
	if err != nil {
		t.Fatalf("Can't create EFI instance: %s", err)
	}
	defer efiVars.Close()

	if _, err = efiVars.GetBootByPartUUID(uuid.New()); err == nil {
		t.Error("Not found error expected")
	}

	bootID, partUUID, err := getCurrentBootPartUUID()
	if err != nil {
		t.Fatalf("Can't get current boot PARTUUID: %s", err)
	}

	id, err := efiVars.GetBootByPartUUID(partUUID)
	if err != nil {
		t.Fatalf("Can't get boot by PARTUUID: %s", err)
	}

	if id != bootID {
		t.Errorf("Wrong boot ID: %04X", id)
	}

	// Check get/set active

	curActive, err := efiVars.GetBootActive(id)
	if err != nil {
		t.Errorf("Can't get boot active: %s", err)
	}

	if err = efiVars.SetBootActive(id, !curActive); err != nil {
		t.Errorf("Can't set boot active: %s", err)
	}

	active, err := efiVars.GetBootActive(id)
	if err != nil {
		t.Errorf("Can't get boot active: %s", err)
	}

	if curActive == active {
		t.Errorf("Wrong boot active value: %v", active)
	}

	// Restore initial active state

	if err = efiVars.SetBootActive(id, curActive); err != nil {
		t.Fatalf("Can't set boot active: %s", err)
	}

	if active, err = efiVars.GetBootActive(id); err != nil {
		t.Errorf("Can't get boot active: %s", err)
	}

	if curActive != active {
		t.Errorf("Wrong boot active value: %v", active)
	}
}

func TestBootOrder(t *testing.T) {
	efiVars, err := efi.New()
	if err != nil {
		t.Fatalf("Can't create EFI instance: %s", err)
	}
	defer efiVars.Close()

	// Read initial value

	initBootOrder, err := efiVars.GetBootOrder()
	if err != nil {
		t.Fatalf("Can't get EFI boot order: %s", err)
	}

	if initBootOrder == nil {
		t.Errorf("Boot order is nil")
	}

	// Delete boot order

	if err = efiVars.DeleteBootOrder(); err != nil {
		t.Fatalf("Can't delete EFI boot order: %s", err)
	}

	// Check that it is deleted

	readBootOrder, err := efiVars.GetBootOrder()
	if err == nil || err != efi.ErrNotFound {
		t.Error("Not found error expected")
	}

	// Restore initial boot order

	if err = efiVars.SetBootOrder(initBootOrder); err != nil {
		t.Fatalf("Can't delete EFI boot order: %s", err)
	}

	// Check that it is restored

	if readBootOrder, err = efiVars.GetBootOrder(); err != nil {
		t.Fatalf("Can't get EFI boot order: %s", err)
	}

	if !reflect.DeepEqual(initBootOrder, readBootOrder) {
		t.Error("Boot order mismatch")
	}
}

func TestBootCurrent(t *testing.T) {
	efiVars, err := efi.New()
	if err != nil {
		t.Fatalf("Can't create EFI instance: %s", err)
	}
	defer efiVars.Close()

	if _, err = efiVars.GetBootCurrent(); err != nil {
		t.Fatalf("Can't get EFI boot current: %s", err)
	}
}

func TestBootNext(t *testing.T) {
	efiVars, err := efi.New()
	if err != nil {
		t.Fatalf("Can't create EFI instance: %s", err)
	}
	defer efiVars.Close()

	var setBootNext uint16 = 0x1111

	if err = efiVars.SetBootNext(setBootNext); err != nil {
		t.Fatalf("Can't set boot next: %s", err)
	}

	getBootNext, err := efiVars.GetBootNext()
	if err != nil {
		t.Errorf("Can't get EFI boot next: %s", err)
	}

	if setBootNext != getBootNext {
		log.Errorf("Wrong boot next value: %d", getBootNext)
	}

	if err = efiVars.DeleteBootNext(); err != nil {
		t.Fatalf("Can't delete boot next: %s", err)
	}

	if _, err = efiVars.GetBootNext(); err == nil || err != efi.ErrNotFound {
		t.Error("Not found error expected")
	}
}

/*******************************************************************************
 * Private
 ******************************************************************************/

func getCurrentBootPartUUID() (id uint16, partUUID uuid.UUID, err error) {
	var output []byte

	if output, err = exec.Command("efibootmgr", "-v").CombinedOutput(); err != nil {
		return 0, uuid.UUID{}, err
	}

	result := strings.Split(string(output), "\n")

	if len(result) == 0 {
		return 0, uuid.UUID{}, errors.New("wrong efibootmgr command output")
	}

	if !strings.HasPrefix(result[0], "BootCurrent: ") {
		return 0, uuid.UUID{}, errors.New("wrong efibootmgr command output")
	}

	idStr := strings.TrimPrefix(result[0], "BootCurrent: ")

	for _, line := range result[1:] {
		if !strings.HasPrefix(line, "Boot"+idStr) {
			continue
		}

		const gptPartUUID = "[a-z0-9]{8}-[a-z0-9]{4}-[1-5][a-z0-9]{3}-[a-z0-9]{4}-[a-z0-9]{12}"

		uuidStr := regexp.MustCompile(gptPartUUID).FindString(strings.TrimPrefix(line, "Boot"+idStr))

		if partUUID, err = uuid.Parse(uuidStr); err != nil {
			return 0, uuid.UUID{}, err
		}

		id64, err := strconv.ParseUint(idStr, 16, 16)
		if err != nil {
			return 0, uuid.UUID{}, err
		}

		return uint16(id64), partUUID, nil
	}

	return 0, uuid.UUID{}, errors.New("no current boot PARTUUID found")
}
