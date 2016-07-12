package vault

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/kopia/kopia/blob"
	"github.com/kopia/kopia/repo"

	"testing"
)

func TestNonColocatedVault(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "vault")
	if err != nil {
		t.Errorf("can't create temp dir: %v", err)
		return
	}
	defer os.RemoveAll(tmpDir)

	verifyVault(
		t,
		filepath.Join(tmpDir, "vlt"),
		filepath.Join(tmpDir, "repo"))
}

func TestColocatedVault(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "vault")
	if err != nil {
		t.Errorf("can't create temp dir: %v", err)
		return
	}
	//defer os.RemoveAll(tmpDir)

	// vault and repository in one storage
	verifyVault(t, tmpDir, tmpDir)
}

func verifyVault(t *testing.T, vaultPath string, repoPath string) {
	vaultStorage := mustCreateFileStorage(t, vaultPath)
	repoStorage := mustCreateFileStorage(t, repoPath)

	vaultCreds, err := Password("foo.bar.baz.foo.bar.baz")
	if err != nil {
		t.Errorf("can't create password credentials: %v", err)
		return
	}

	otherVaultCreds, err := Password("foo.bar.baz.foo.bar.baz0")
	if err != nil {
		t.Errorf("can't create password credentials: %v", err)
		return
	}

	vaultFormat := &Format{
		Encryption: "aes-256",
		Checksum:   "hmac-sha-256",
	}

	repoFormat := &repo.Format{
		Version:      "1",
		MaxBlobSize:  1000000,
		ObjectFormat: "sha256",
		Secret:       []byte{1, 2, 3},
	}

	_, err = repo.NewRepository(repoStorage, repoFormat)
	if err != nil {
		t.Errorf("can't create repository: %v", err)
		return
	}

	v1, err := Create(vaultStorage, vaultFormat, vaultCreds, repoStorage, repoFormat)
	if err != nil {
		t.Errorf("can't create vault: %v", err)
		return
	}

	v2, err := Open(vaultStorage, vaultCreds)
	if err != nil {
		t.Errorf("can't open vault: %v", err)
		return
	}

	tok1, err := v1.Token()
	if err != nil {
		t.Errorf("error getting token1 %v", err)
	}
	tok2, err := v2.Token()
	if err != nil {
		t.Errorf("error getting token2 %v", err)
	}

	v3, err := OpenWithToken(tok1)
	if err != nil {
		t.Errorf("can't open vault with token: %v", err)
		return
	}

	if tok1 != tok2 {
		t.Errorf("tokens don't match: %v vs %v", tok1, tok2)
	}

	_, err = Open(vaultStorage, otherVaultCreds)
	if err == nil {
		t.Errorf("unexpectedly opened vault with invalid credentials")
		return
	}

	if err := v1.Put("foo", []byte("test1")); err != nil {
		t.Errorf("error putting: %v", err)
	}
	if err := v2.Put("bar", []byte("test2")); err != nil {
		t.Errorf("error putting: %v", err)
	}
	if err := v1.Put("baz", []byte("test3")); err != nil {
		t.Errorf("error putting: %v", err)
	}

	r1, err := v1.OpenRepository()
	if err != nil {
		t.Errorf("error opening v1 repository: %v", err)
	}

	r2, err := v2.OpenRepository()
	if err != nil {
		t.Errorf("error opening v1 repository: %v", err)
	}

	w1 := r1.NewWriter()
	w1.Write([]byte("foo"))
	oid1, err := w1.Result(true)
	if err != nil {
		t.Errorf("Result error: %v", err)
	}

	w2 := r2.NewWriter()
	w2.Write([]byte("bar"))
	oid2, err := w2.Result(true)
	if err != nil {
		t.Errorf("Result error: %v", err)
	}

	saved1, err := v1.SaveObjectID(oid1)
	if err != nil {
		t.Errorf("error saving object ID: %v", err)
	}
	saved2, err := v2.SaveObjectID(oid1)
	if err != nil {
		t.Errorf("error saving object ID: %v", err)
	}
	saved3, err := v1.SaveObjectID(oid2)
	if err != nil {
		t.Errorf("error saving object ID: %v", err)
	}
	if saved1 != saved2 {
		t.Errorf("save IDs don't match: %v %v", saved1, saved2)
	}
	if saved1 == saved3 {
		t.Errorf("save IDs do match: %v, but should not", saved1)
	}

	// Verify contents of vault items for both created and opened vault.
	for _, v := range []*Vault{v1, v2, v3} {
		rf := v.RepositoryFormat()
		if !reflect.DeepEqual(rf, repoFormat) {
			t.Errorf("invalid repository format: %v, but got %v", repoFormat, rf)
		}
		assertVaultItem(t, v, "foo", "test1")
		assertVaultItem(t, v, "bar", "test2")
		assertVaultItem(t, v, "baz", "test3")
		assertVaultItemNotFound(t, v, "x")

		assertVaultItems(t, v, "x", nil)
		assertVaultItems(t, v, "f", []string{"foo"})
		assertVaultItems(t, v, "ba", []string{"bar", "baz"})
		assertVaultItems(t, v, "be", nil)
		assertVaultItems(t, v, "baz", []string{"baz"})
		assertVaultItems(t, v, "bazx", nil)

		assertReservedName(t, v, formatBlockID)
		assertReservedName(t, v, repositoryConfigBlockID)

		oid, err := v.GetObjectID(saved1)
		if err != nil {
			t.Errorf("error getting object ID: %v", err)
		} else {
			if oid != oid1 {
				t.Errorf("got invalid object ID: %v expected %v", oid, oid1)
			}
		}

		oid, err = v.GetObjectID(saved3)
		if err != nil {
			t.Errorf("error getting object ID: %v", err)
		} else {
			if oid != oid2 {
				t.Errorf("got invalid object ID: %v expected %v", oid, oid2)
			}
		}

		_, err = v.GetObjectID("no-such-saved-object-id")
		if err != ErrItemNotFound {
			t.Errorf("invalid error, got %v, but expected %v", err, ErrItemNotFound)
		}
	}

	// Make sure repository is shared, by checking that data written in one can be read by the other.
	assertRepositoryItem(t, r1, oid1, "foo")
	assertRepositoryItem(t, r1, oid2, "bar")
	assertRepositoryItem(t, r2, oid1, "foo")
	assertRepositoryItem(t, r2, oid2, "bar")

	v1.Remove("bar")

	for _, v := range []*Vault{v1, v2} {
		assertVaultItem(t, v, "foo", "test1")
		assertVaultItemNotFound(t, v, "bar")
		assertVaultItem(t, v, "baz", "test3")

		assertVaultItems(t, v, "x", nil)
		assertVaultItems(t, v, "f", []string{"foo"})
		assertVaultItems(t, v, "ba", []string{"baz"})
		assertVaultItems(t, v, "be", nil)
		assertVaultItems(t, v, "baz", []string{"baz"})
		assertVaultItems(t, v, "bazx", nil)
	}

	if err := v1.Close(); err != nil {
		t.Errorf("v1.Close() error: %v", err)
	}
	if err := v2.Close(); err != nil {
		t.Errorf("v2.Close() error: %v", err)
	}
}

func assertVaultItem(t *testing.T, v *Vault, itemID string, expectedData string) {
	b, err := v.Get(itemID)
	if err != nil {
		t.Errorf("error getting item %v: %v", itemID, err)
	}

	bs := string(b)
	if bs != expectedData {
		t.Errorf("invalid data for '%v': expected: %v but got %v", itemID, expectedData, bs)
	}
}

func assertRepositoryItem(t *testing.T, repository repo.Repository, oid repo.ObjectID, expectedData string) {
	r, err := repository.Open(oid)
	if err != nil {
		t.Errorf("error opening item %v: %v", oid, err)
		return
	}

	b, err := ioutil.ReadAll(r)
	if err != nil {
		t.Errorf("error reading item %v: %v", oid, err)
		return
	}

	bs := string(b)
	if bs != expectedData {
		t.Errorf("invalid data for '%v': expected: %v but got %v", oid, expectedData, bs)
	}
}

func assertVaultItemNotFound(t *testing.T, v *Vault, itemID string) {
	result, err := v.Get(itemID)
	if err != ErrItemNotFound {
		t.Errorf("invalid error getting item %v: %v (result=%v), but expected %v", itemID, err, result, ErrItemNotFound)
	}
}

func assertReservedName(t *testing.T, v *Vault, itemID string) {
	_, err := v.Get(itemID)
	assertReservedNameError(t, "Get", itemID, err)
	assertReservedNameError(t, "Put", itemID, v.Put(itemID, nil))
	assertReservedNameError(t, "Remove", itemID, v.Remove(itemID))
}

func assertReservedNameError(t *testing.T, method string, itemID string, err error) {
	if err == nil {
		t.Errorf("call did not fail: %v(%v)", method, itemID)
		return
	}

	if !strings.Contains(err.Error(), "invalid vault item name") {
		t.Errorf("call did not fail the right way: %v(%v), was: %v", method, itemID, err)
	}
}

func assertVaultItems(t *testing.T, v *Vault, prefix string, expected []string) {
	res, err := v.List(prefix)
	if err != nil {
		t.Errorf("error listing items beginning with %v: %v", prefix, err)
	}

	if !reflect.DeepEqual(expected, res) {
		t.Errorf("unexpected items returned for prefix '%v': %v, but expected %v", prefix, res, expected)
	}
}

func mustCreateFileStorage(t *testing.T, path string) blob.Storage {
	os.MkdirAll(path, 0700)
	s, err := blob.NewFSStorage(&blob.FSStorageOptions{
		Path: path,
	})
	if err != nil {
		t.Errorf("can't create file storage: %v", err)
	}
	return s
}