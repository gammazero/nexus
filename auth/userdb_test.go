package auth

import "testing"

func newUserDB() UserDB {
	userDB := NewStaticUserDB("static")
	userDB.CreateUser("jdoe", map[string]string{
		"role": "user", "secret": "password1"})
	userDB.CreateUser("jbond", map[string]string{
		"role": "admin", "secret": "agent007"})
	userDB.CreateUser("anonymous", nil)
	return userDB
}

func TestUserDB(t *testing.T) {
	userDB := newUserDB()
	if _, err := userDB.ReadUserInfo("missing"); err == nil {
		t.Fatal("expected error reading info for missing user ")
	}

	err := userDB.CreateUser("jbond", map[string]string{"role": "admin"})
	if err == nil {
		t.Fatal("expected error creating existing")
	}

	info, err := userDB.ReadUserInfo("jbond")
	if err != nil {
		t.Fatal("err reading user info:", err)
	}
	if info["role"] != "admin" {
		t.Fatal("got wrong user info from DB")
	}

	if err = userDB.UpdateUserInfo("missing", "role", "root"); err == nil {
		t.Fatal("expected error updating info for missing user ")
	}

	if err = userDB.UpdateUserInfo("jbond", "role", "root"); err != nil {
		t.Fatal("err updating user info:", err)
	}

	info, err = userDB.ReadUserInfo("jbond")
	if info["role"] != "root" {
		t.Fatal("got wrong user info from DB after update")
	}

	if err = userDB.UpdateUserInfo("jbond", "role", ""); err != nil {
		t.Fatal("err updating user info:", err)
	}
	info, err = userDB.ReadUserInfo("jbond")
	if _, ok := info["role"]; ok {
		t.Fatal("should not have role for user jbond")
	}

	if err = userDB.DeleteUser("missing"); err == nil {
		t.Fatal("expected error deleting missing user ")
	}

	if err = userDB.DeleteUser("jbond"); err != nil {
		t.Fatal("error deleting user: ", err)
	}
	if _, err := userDB.ReadUserInfo("jbond"); err == nil {
		t.Fatal("expected error reading deleted user")
	}
}
