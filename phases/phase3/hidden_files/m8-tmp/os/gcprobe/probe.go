package main

import (
	"fmt"
	"os"
)

func main() {
	// empty-name ops
	_, err := os.Open("")
	fmt.Printf("Open empty: %v\n", err)
	_, err = os.Stat("")
	fmt.Printf("Stat empty: %v\n", err)
	fmt.Printf("MkdirAll empty: %v\n", os.MkdirAll("", 0755))
	fmt.Printf("RemoveAll empty: %v\n", os.RemoveAll(""))
	fmt.Printf("Mkdir empty: %v\n", os.Mkdir("", 0755))
	fmt.Printf("Remove empty: %v\n", os.Remove(""))
	fmt.Printf("Chdir empty: %v\n", os.Chdir(""))
	// negative offset
	f, _ := os.CreateTemp("", "p*")
	defer os.Remove(f.Name())
	_, err = f.ReadAt(make([]byte, 4), -1)
	fmt.Printf("ReadAt neg: %v\n", err)
	_, err = f.WriteAt([]byte("x"), -1)
	fmt.Printf("WriteAt neg: %v\n", err)
	_, err = f.Seek(-5, 0)
	fmt.Printf("Seek neg: %v\n", err)
	// nil file
	var nf *os.File
	_, err = nf.Read(make([]byte, 4))
	fmt.Printf("nil Read: %v\n", err)
	fmt.Printf("nil Close: %v\n", nf.Close())
	// double close
	f2, _ := os.CreateTemp("", "p*")
	defer os.Remove(f2.Name())
	f2.Close()
	fmt.Printf("double Close: %v\n", f2.Close())
	// seek on dir
	d, _ := os.MkdirTemp("", "d*")
	defer os.RemoveAll(d)
	df, _ := os.Open(d)
	defer df.Close()
	off, err := df.Seek(0, 0)
	fmt.Printf("dir Seek(0,0): %d %v\n", off, err)
	off, err = df.Seek(5, 0)
	fmt.Printf("dir Seek(5,0): %d %v\n", off, err)
	// read on dir
	_, err = df.Read(make([]byte, 4))
	fmt.Printf("dir Read: %v\n", err)
	// NewFile foreign
	fmt.Printf("NewFile(99): %v\n", os.NewFile(99, "x") == nil)
	// ExpandEnv
	os.Setenv("M8_TEST_VAR", "hello")
	fmt.Printf("ExpandEnv: %q\n", os.ExpandEnv("a${M8_TEST_VAR}b${UNSET_M8}c"))
	os.Unsetenv("M8_TEST_VAR")
	// UserHomeDir etc
	hd, _ := os.UserHomeDir()
	fmt.Printf("UserHomeDir nonempty: %v\n", hd != "")
	// TempDir
	fmt.Printf("TempDir: %q\n", os.TempDir())
	// Getwd PWD kludge: set bogus PWD
	os.Setenv("PWD", "/nonexistent-m8-pwd")
	wd, _ := os.Getwd()
	fmt.Printf("Getwd bogus PWD ignored: %v\n", wd != "/nonexistent-m8-pwd")
	os.Unsetenv("PWD")
	// SameFile
	fi1, _ := os.Stat(f.Name())
	fi2, _ := f.Stat()
	fmt.Printf("SameFile: %v\n", os.SameFile(fi1, fi2))
	// ReadFile missing
	_, err = os.ReadFile("/nonexistent-m8-xyz/file")
	fmt.Printf("ReadFile missing: IsNotExist=%v\n", os.IsNotExist(err))
	// Symlink + SameFile via Link
	os.Symlink(f.Name(), f.Name()+".lnk")
	defer os.Remove(f.Name() + ".lnk")
	li, _ := os.Lstat(f.Name() + ".lnk")
	fmt.Printf("Lstat symlink mode symlink: %v\n", li.Mode()&os.ModeSymlink != 0)
	// hostname nonempty
	hn, _ := os.Hostname()
	fmt.Printf("Hostname nonempty: %v\n", hn != "")
}
