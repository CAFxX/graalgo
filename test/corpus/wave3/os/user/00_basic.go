package main

// L11 (REVIEW-2026-11-18-05): os/user — zero corpus imports before
// this program. Looking up users and groups: Current, Lookup by
// name, LookupId, LookupGroup, and the error paths on unknown names.

import (
	"fmt"
	"os/user"
)

func main() {
	cur, err := user.Current()
	fmt.Printf("current-err=%v\n", err != nil)
	if err == nil {
		fmt.Printf("current username=%q uid=%q gid=%q homedir-set=%v\n",
			cur.Username, cur.Uid, cur.Gid, cur.HomeDir != "")
	}
	root, err := user.Lookup("root")
	fmt.Printf("root-err=%v\n", err != nil)
	if err == nil {
		fmt.Printf("root uid=%q gid=%q\n", root.Uid, root.Gid)
	}
	zero, err := user.LookupId("0")
	fmt.Printf("uid0-err=%v\n", err != nil)
	if err == nil {
		fmt.Printf("uid0 username=%q\n", zero.Username)
	}
	_, err = user.Lookup("nosuchuser-1805")
	fmt.Printf("nosuch-err=%v\n", err != nil)
	_, err = user.LookupId("999999")
	fmt.Printf("nosuchid-err=%v\n", err != nil)
	g, err := user.LookupGroup("root")
	fmt.Printf("group-root-err=%v\n", err != nil)
	if err == nil {
		fmt.Printf("group-root gid=%q\n", g.Gid)
	}
	_, err = user.LookupGroup("nosuchgroup-1805")
	fmt.Printf("group-nosuch-err=%v\n", err != nil)
}
