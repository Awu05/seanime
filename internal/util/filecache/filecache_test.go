package filecache

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/stretchr/testify/assert"
)

func TestRemoveAllBy(t *testing.T) {
	cacher, err := NewCacher(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatal(err)
	}

	mediaInfoBucket := NewBucket("mediastream_mediainfo_test", time.Hour)
	otherBucket := NewBucket("other", time.Hour)
	if err := cacher.Set(mediaInfoBucket, "key", "stale"); err != nil {
		t.Fatal(err)
	}
	if err := cacher.Set(otherBucket, "key", "keep"); err != nil {
		t.Fatal(err)
	}

	if err := cacher.RemoveAllBy(func(filename string) bool {
		return strings.HasPrefix(filename, "mediastream_mediainfo_")
	}); err != nil {
		t.Fatal(err)
	}

	var value string
	found, err := cacher.Get(mediaInfoBucket, "key", &value)
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("removed cache remained in memory")
	}

	found, err = cacher.Get(otherBucket, "key", &value)
	if err != nil {
		t.Fatal(err)
	}
	if !found || value != "keep" {
		t.Fatalf("unrelated cache was removed: found=%v value=%q", found, value)
	}
}

func TestCacherFunctions(t *testing.T) {
	cacher, err := NewCacher(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("Failed to create cacher: %v", err)
	}

	bucket := Bucket{
		name: "test",
		ttl:  10 * time.Second,
	}

	keys := []string{"key1", "key2", "key3"}

	type valStruct = struct {
		Name string
	}

	values := []*valStruct{
		{
			Name: "value1",
		},
		{
			Name: "value2",
		},
		{
			Name: "value3",
		},
	}

	for i, key := range keys {
		err = cacher.Set(bucket, key, values[i])
		if err != nil {
			t.Fatalf("Failed to set the value: %v", err)
		}
	}

	allVals, err := GetAll[*valStruct](cacher, bucket)
	if err != nil {
		t.Fatalf("Failed to get all values: %v", err)
	}

	if len(allVals) != len(keys) {
		t.Fatalf("Failed to get all values: expected %d, got %d", len(keys), len(allVals))
	}

	spew.Dump(allVals)
}

func TestCacherSetAndGet(t *testing.T) {
	cacher, err := NewCacher(filepath.Join(t.TempDir(), "cache"))

	bucket := Bucket{
		name: "test",
		ttl:  4 * time.Second,
	}
	key := "key"
	value := struct {
		Name string
	}{
		Name: "value",
	}
	// Add "key" -> value to the bucket, with a TTL of 4 seconds
	err = cacher.Set(bucket, key, value)
	if err != nil {
		t.Fatalf("Failed to set the value: %v", err)
	}

	var out struct {
		Name string
	}
	// Get the value of "key" from the bucket, it shouldn't be expired
	found, err := cacher.Get(bucket, key, &out)
	if err != nil {
		t.Errorf("Failed to get the value: %v", err)
	}
	if !found || !assert.Equal(t, value, out) {
		t.Errorf("Failed to get the correct value. Expected %v, got %v", value, out)
	}

	spew.Dump(out)

	time.Sleep(3 * time.Second)

	// Get the value of "key" from the bucket again, it shouldn't be expired
	found, err = cacher.Get(bucket, key, &out)
	if !found {
		t.Errorf("Failed to get the value")
	}
	if !found || out != value {
		t.Errorf("Failed to get the correct value. Expected %v, got %v", value, out)
	}

	spew.Dump(out)

	// Spin up a goroutine to set "key2" -> value2 to the bucket, with a TTL of 1 second
	// cacher should be thread-safe
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		key2 := "key2"
		value2 := struct {
			Name string
		}{
			Name: "value2",
		}
		var out2 struct {
			Name string
		}
		err = cacher.Set(bucket, key2, value2)
		if err != nil {
			t.Errorf("Failed to set the value: %v", err)
		}

		found, err = cacher.Get(bucket, key2, &out2)
		if err != nil {
			t.Errorf("Failed to get the value: %v", err)
		}

		if !found || !assert.Equal(t, value2, out2) {
			t.Errorf("Failed to get the correct value. Expected %v, got %v", value2, out2)
		}

		_ = cacher.Delete(bucket, key2)

		spew.Dump(out2)

	}()

	time.Sleep(2 * time.Second)

	// Get the value of "key" from the bucket, it should be expired
	found, _ = cacher.Get(bucket, key, &out)
	if found {
		t.Errorf("Failed to delete the value")
		spew.Dump(out)
	}

	wg.Wait()

}

func TestGetPermWithAge(t *testing.T) {
	cacher, err := NewCacher(filepath.Join(t.TempDir(), "cache"))
	if err != nil {
		t.Fatalf("Failed to create cacher: %v", err)
	}

	bucket := NewPermanentBucket("test-age")

	var out string
	found, _, err := cacher.GetPermWithAge(bucket, "missing", &out)
	if err != nil {
		t.Fatalf("unexpected error for missing key: %v", err)
	}
	if found {
		t.Fatal("expected missing key to not be found")
	}

	if err := cacher.SetPerm(bucket, "key", "value"); err != nil {
		t.Fatalf("Failed to set the value: %v", err)
	}

	found, age, err := cacher.GetPermWithAge(bucket, "key", &out)
	if err != nil {
		t.Fatalf("Failed to get the value: %v", err)
	}
	if !found || out != "value" {
		t.Fatalf("Failed to get the correct value. Expected %v, got %v", "value", out)
	}
	if age < 0 || age > time.Second {
		t.Fatalf("expected a freshly-set entry to have a near-zero age, got %v", age)
	}

	// Backdate the entry to simulate a stale cache without sleeping in the test.
	store := cacher.stores[bucket.name]
	store.mu.Lock()
	old := time.Now().Add(-2 * time.Hour)
	store.data["key"].UpdatedAt = &old
	store.mu.Unlock()

	found, age, err = cacher.GetPermWithAge(bucket, "key", &out)
	if err != nil {
		t.Fatalf("Failed to get the value: %v", err)
	}
	if !found {
		t.Fatal("expected the backdated entry to still be found (permanent bucket)")
	}
	if age < 2*time.Hour {
		t.Fatalf("expected the backdated entry's age to reflect the backdate, got %v", age)
	}
}
