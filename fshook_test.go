package dugong

import (
	"bufio"
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	fieldName  = "my_field"
	fieldValue = "my_value"
)

func TestFSHook(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	logger.WithField(fieldName, fieldValue).Info("Info message")

	wait()

	checkLogFile(t, hook.path, "info")
}

func TestFsHookInterleaved(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	logger.WithField("counter", 0).Info("message")
	logger.WithField("counter", 1).Warn("message")
	logger.WithField("counter", 2).Error("message")
	logger.WithField("counter", 3).Warn("message")
	logger.WithField("counter", 4).Info("message")

	wait()

	file, err := os.Open(hook.path)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		dataCounter := int(data["counter"].(float64))
		if count != dataCounter {
			t.Fatalf("Counter: want %d got %d", count, dataCounter)
		}
		count++
	}

	if count != 5 {
		t.Fatalf("Lines: want 5 got %d", count)
	}
}

func TestFSHookMultiple(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	for i := 0; i < 100; i++ {
		logger.WithField("counter", i).Info("message")
	}

	wait()

	file, err := os.Open(hook.path)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		dataCounter := int(data["counter"].(float64))
		if count != dataCounter {
			t.Fatalf("Counter: want %d got %d", count, dataCounter)
		}
		count++
	}

	if count != 100 {
		t.Fatalf("Lines: want 100 got %d", count)
	}
}

func TestFSHookConcurrent(t *testing.T) {
	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)

		go func(counter int) {
			defer wg.Done()
			logger.WithField("counter", counter).Info("message")
		}(i)
	}

	wg.Wait()
	wait()

	file, err := os.Open(hook.path)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
	}

	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("Failed to parse JSON: %v", err)
		}
		count++
	}

	if count != 100 {
		t.Fatalf("Lines: want 100 got %d", count)
	}
}

func TestFSHookGZ(t *testing.T) {
	oldumask := syscall.Umask(0)
	defer syscall.Umask(oldumask)

	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("Failed to load location UTC: %s", err)
	}

	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()
	hook.scheduler = &DailyRotationSchedule{
		GZip: true,
	}

	// Time ticks from 23:50 to 00:10 in 1 minute increments. Log each tick as 'counter'.
	minutesGoneBy := 0
	currentTime = func() time.Time {
		minutesGoneBy++
		return time.Date(2016, 10, 26, 23, 50+minutesGoneBy, 00, 0, loc)
	}
	for i := 0; i < 20; i++ {
		t := time.Date(2016, 10, 26, 23, 50+i, 00, 0, loc)
		logger.WithField("counter", i).Info("BASE " + t.Format(time.ANSIC))
	}

	wait()

	f, err := os.Open(hook.path + ".2016-10-26")
	if err == nil {
		t.Fatalf("Failed to remove original file after gziping\n")
	}

	f, err = os.Open(hook.path + ".2016-10-26.gz")
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	s, err := f.Stat()
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	if s.Mode() != hook.logFilePerm {
		t.Fatalf("gzipped file permissions incorrect, wanted %v got %v", hook.logFilePerm, s.Mode())
	}

	f.Close()
}

func TestDailySchedule(t *testing.T) {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("Failed to load location UTC: %s", err)
	}

	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()
	hook.scheduler = &DailyRotationSchedule{}

	// Time ticks from 23:50 to 00:10 in 1 minute increments. Log each tick as 'counter'.
	minutesGoneBy := 0
	currentTime = func() time.Time {
		minutesGoneBy++
		return time.Date(2016, 10, 26, 23, 50+minutesGoneBy, 00, 0, loc)
	}
	for i := 0; i < 20; i++ {
		t := time.Date(2016, 10, 26, 23, 50+i, 00, 0, loc)
		logger.WithField("counter", i).Info("BASE " + t.Format(time.ANSIC))
	}

	wait()

	// fshook.log.2016-10-26 should have 0 -> 9
	checkFileHasSequentialCounts(t, hook.path+".2016-10-26", 0, 9)

	// fshook.log should have 10 -> 19 inclusive
	checkFileHasSequentialCounts(t, hook.path, 10, 19)
}

func TestDailyScheduleMultipleRotations(t *testing.T) {
	loc, err := time.LoadLocation("UTC")
	if err != nil {
		t.Fatalf("Failed to load location UTC: %s", err)
	}

	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()
	hook.scheduler = &DailyRotationSchedule{}

	// Time ticks every 12 hours from 13:37 -> 01:37 -> 13:37 -> ...
	hoursGoneBy := 0
	currentTime = func() time.Time {
		hoursGoneBy += 12
		// Start from 10/29 01:37
		return time.Date(2016, 10, 28, 13+hoursGoneBy, 37, 00, 0, loc)
	}
	// log 8 lines
	for i := 0; i < 8; i++ {
		ts := time.Date(2016, 10, 28, 13+((i+1)*12), 37, 00, 0, loc)
		logger.WithField("counter", i).Infof("The time is now %s", ts)
	}

	wait()

	// fshook.log.2016-10-29 should have 0-1
	checkFileHasSequentialCounts(t, hook.path+".2016-10-29", 0, 1)

	// fshook.log.2016-10-30 should have 2-3
	checkFileHasSequentialCounts(t, hook.path+".2016-10-30", 2, 3)

	// fshook.log.2016-10-31 should have 4-5
	checkFileHasSequentialCounts(t, hook.path+".2016-10-31", 4, 5)

	// fshook.log should have 6-7 (current day is 11/01)
	checkFileHasSequentialCounts(t, hook.path, 6, 7)
}

func TestFilePermDefault(t *testing.T) {
	checkFilePerm(t, nil)
}

func TestFilePermAllRead(t *testing.T) {
	var mode os.FileMode = 0644
	checkFilePerm(t, &mode)
}

func TestFilePermAllReadWrite(t *testing.T) {
	var mode os.FileMode = 0666
	checkFilePerm(t, &mode)
}

func checkFilePerm(t *testing.T, mode *os.FileMode) {
	// Set the umask to 0, otherwise the effective permissions of the file
	// we create will be the ones we asked for with the permssions in the
	// umask removed, which is not so useful for testing
	oldumask := syscall.Umask(0)
	defer syscall.Umask(oldumask)

	logger, hook, wait, teardown := setupLogHook(t)
	defer teardown()

	if mode != nil {
		hook.SetFilePerm(*mode)
	}

	logger.Info("log log log")

	wait()

	f, err := os.Open(hook.path)
	if err != nil {
		t.Fatalf("Failed to open log file: %v", err)
	}
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}
	f.Close()

	var expectedMode os.FileMode
	if mode != nil {
		expectedMode = *mode
	} else {
		expectedMode = 0660
	}

	if info.Mode() != expectedMode {
		t.Fatalf("Wanted file mode %v but got %v", expectedMode, info.Mode())
	}
}

// checkFileHasSequentialCounts based on a JSON "counter" key being a monotonically
// incrementing integer. from and to are both inclusive.
func checkFileHasSequentialCounts(t *testing.T, filepath string, from, to int) {
	t.Logf("checkFileHasSequentialCounts(%s,%d,%d)", filepath, from, to)

	file, err := os.Open(filepath)
	if err != nil {
		t.Fatalf("Failed to open file: %v", err)
		return
	}

	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := from
	for scanner.Scan() {
		data := make(map[string]interface{})
		if err := json.Unmarshal([]byte(scanner.Text()), &data); err != nil {
			t.Fatalf("%s : Failed to parse JSON: %v", file.Name(), err)
		}
		dataCounter := int(data["counter"].(float64))
		t.Logf("%s want %d got %d", file.Name(), count, dataCounter)
		if count != dataCounter {
			t.Fatalf("%s : Counter: want %d got %d", file.Name(), count, dataCounter)
		}

		count++
	}
	count-- // never hit the next value

	if count != to {
		t.Fatalf("%s EOF: Want count %d got %d", file.Name(), to, count)
	}
}

func setupLogHook(t *testing.T) (logger *log.Logger, hook *fsHook, wait func(), teardown func()) {
	dir, err := ioutil.TempDir("", "TestFSHook")
	if err != nil {
		t.Fatalf("Failed to make temporary directory: %v", err)
	}

	path := filepath.Join(dir, "fshook.log")

	hook = NewFSHook(path, nil, nil).(*fsHook)

	logger = log.New()
	logger.Hooks.Add(hook)

	wait = func() {
		for atomic.LoadInt32(&hook.queueSize) != 0 {
			runtime.Gosched()
		}
	}

	teardown = func() {
		os.RemoveAll(dir)
	}

	return
}

func checkLogFile(t *testing.T, path, expectedLevel string) {
	contents, err := ioutil.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	data := make(map[string]interface{})
	if err := json.Unmarshal(contents, &data); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if data["level"] != expectedLevel {
		t.Fatalf("level: want %q got %q", expectedLevel, data["level"])
	}

	if data[fieldName] != fieldValue {
		t.Fatalf("%s: want %q got %q", fieldName, fieldValue, data[fieldName])
	}
}
