package main

import (
	"bytes"
	"compress/lzw"
	"crypto/sha1"
	"encoding/gob"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

var DEBUG bool = false

const BASEDIR = "./.gat"

var IGNORE_FOLDERS = []string{".git", ".gat"}

type Commit struct {
	TreeHash     string
	ParentCommit string
	Message      string
	Timestamp    int64
}

func (c Commit) String() string {
	var res string
	res += fmt.Sprintf("Tree Hash\t: %v\n", c.TreeHash)
	res += fmt.Sprintf("Parent Commit\t: %v\n", c.ParentCommit)
	res += fmt.Sprintf("Time\t\t: %v\n", time.Unix(c.Timestamp, 0))
	res += fmt.Sprintf("Message\t\t: %v\n", c.Message)
	return res
}

// returns the path to an object based on its hash
func getObjectPath(hash string) string {
	return filepath.Join(BASEDIR, "objects", hash[:2], hash[2:])
}

// returns the path to an object folder based on its hash
func getObjectFolderPath(hash string) string {
	return filepath.Join(BASEDIR, "objects", hash[:2])
}

// creates a compressed blob file in the .git/objects folder
func createBlob(path string) (*string, error) {
	// 1. hash file and compress file at the same time
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("unable to read file: %v", err)
		return nil, err
	}
	defer f.Close()

	// temp file
	tempFileName := BASEDIR + "/temp/" + randomString(6)
	tempFile, err := os.Create(tempFileName)
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	lzwWriter := lzw.NewWriter(tempFile, lzw.LSB, 8)
	defer lzwWriter.Close()
	hasher := sha1.New()
	buf := make([]byte, 1024)
	for {
		n, err := f.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Printf("unable to read file: %v", err)
			return nil, err
		}
		if n > 0 {
			hasher.Write(buf[:n])
			lzwWriter.Write(buf[:n])
		}
	}
	lzwWriter.Close()
	checksum := hex.EncodeToString(hasher.Sum(nil))
	if len(checksum) != 40 {
		return nil, fmt.Errorf("failed to hash file, expected hash length 40 got %v", len(checksum))
	}

	// 2. create subfolder for blob object
	subfolderPath := getObjectFolderPath(checksum)
	err = os.MkdirAll(subfolderPath, os.ModePerm)
	if err != nil {
		return nil, fmt.Errorf("failed to create blob subfolder \"%v\"", subfolderPath)
	}

	err = os.Rename(tempFileName, getObjectPath(checksum))
	if err != nil {
		return nil, err
	}

	return &checksum, nil
}

// creates a root tree for the current directory
func createTree(folderPath string) (*string, error) {
	tempFileName := BASEDIR + "/temp/" + randomString(6)
	tempFile, err := os.Create(tempFileName)
	if err != nil {
		return nil, err
	}

	defer tempFile.Close()

	dir, err := os.Open(folderPath)
	if err != nil {
		log.Fatal(err)
	}
	defer dir.Close()

	files, err := dir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		filePath := folderPath + "/" + f.Name()
		if f.IsDir() {
			if slices.Contains[[]string](IGNORE_FOLDERS, f.Name()) {
				continue
			}
			checksum, err := createTree(filePath)
			if err != nil {
				log.Fatalf("Failed to create tree for folder %v", filePath)
			}
			row := fmt.Sprintf("%v\t%v\t%v\n", "tree", *checksum, f.Name())
			tempFile.WriteString(row)
		} else {
			checksum, err := createBlob(filePath)
			if err != nil {
				log.Fatalf("Failed to create blob for file %v, error : %v\n", filePath, err)
			}
			row := fmt.Sprintf("%v\t%v\t%v\n", "blob", *checksum, f.Name())
			tempFile.WriteString(row)
		}
	}
	tempFile.Close()
	checksum, err := createBlob(tempFileName)
	if !DEBUG {
		os.Remove(tempFileName)
	}

	if err != nil {
		log.Fatalf("failed to create blob for the tree file \"%v\" temp file \"%v\"\n", folderPath, tempFileName)
	}
	return checksum, nil
}

// create a commit from a tree
func createCommit(treeHash string, parentCommitHash string, message string) (*string, error) {
	commit := Commit{
		TreeHash:     treeHash,
		ParentCommit: parentCommitHash,
		Message:      message,
		Timestamp:    time.Now().Unix(),
	}

	// temp file
	tempFileName := BASEDIR + "/temp/" + randomString(6)
	tempFile, err := os.Create(tempFileName)
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()

	e := gob.NewEncoder(tempFile)
	err = e.Encode(commit)
	tempFile.Close()
	if err != nil {
		return nil, fmt.Errorf("failed to seriliaze commit, error %v", err)
	}

	commitHash, err := createBlob(tempFileName)

	updateCurrentHead(*commitHash)

	return commitHash, err
}

func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}

// prepares the necessary folders for a git repo
func prepareDirs() error {
	err := os.MkdirAll(BASEDIR, os.ModePerm)
	if err != nil {
		return err
	}

	err = os.MkdirAll(BASEDIR+"/objects", os.ModePerm)
	if err != nil {
		return err
	}
	err = os.MkdirAll(BASEDIR+"/temp", os.ModePerm)
	if err != nil {
		return err
	}
	err = os.MkdirAll(BASEDIR+"/refs", os.ModePerm)
	if err != nil {
		return err
	}
	err = os.MkdirAll(BASEDIR+"/refs/heads", os.ModePerm)
	if err != nil {
		return err
	}
	_, err = os.OpenFile(BASEDIR+"/refs/heads/main", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(BASEDIR+"/HEAD", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	buf := make([]byte, 5)
	if _, err := f.Read(buf); err == io.EOF {
		f.WriteString(filepath.Join("refs", "heads", "main"))
	}
	err = os.MkdirAll(BASEDIR+"/refs/tags", os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

// read object file
func readObjectFile(path string) (*[]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Printf("unable to read file: %v", err)
		return nil, err
	}
	defer f.Close()

	reader := lzw.NewReader(f, lzw.LSB, 8)
	defer reader.Close()

	decompressedData, err := io.ReadAll(reader)
	if err != nil {
		fmt.Printf("failed to decompress object file, error : %v\n", err)
		return nil, err
	}

	return &decompressedData, nil
}

// return the head (commit hash) of the current branch
func getCurrentHead() (*string, error) {
	content, err := os.ReadFile(BASEDIR + "/HEAD")
	if err != nil {
		return nil, err
	}
	headFilePath := string(content)

	branchContent, err := os.ReadFile(BASEDIR + "/" + headFilePath)
	if err != nil {
		return nil, err
	}
	currentCommitHash := string(branchContent)

	return &currentCommitHash, nil
}

// get current branch name
func getCurrentBranch() (*string, error) {
	content, err := os.ReadFile(BASEDIR + "/HEAD")
	if err != nil {
		return nil, err
	}
	headFilePath := string(content)

	// strip the base part
	basePath := filepath.Join("refs", "heads")
	branchName := headFilePath[len(basePath)+1:]

	return &branchName, nil
}

// update head of current branch to new commit
func updateCurrentHead(newCommitHash string) error {
	content, err := os.ReadFile(BASEDIR + "/HEAD")
	if err != nil {
		return err
	}
	headFilePath := string(content)

	// Open the branchFile with write-only and truncate flags
	branchFile, err := os.OpenFile(BASEDIR+"/"+headFilePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer branchFile.Close()

	// Write new content to the file
	_, err = branchFile.WriteString(newCommitHash)
	if err != nil {
		return err
	}

	return nil
}

// switch head to new branch
func switchHead(branchName string) error {
	headFile, err := os.OpenFile(BASEDIR+"/HEAD", os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer headFile.Close()

	headFile.WriteString("refs/heads/" + branchName)

	return nil
}

// print current repo status
func status() error {
	currentBranch, err := getCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to get current branch, error : %v", err)
	}
	fmt.Printf("Current branch : %v\n", *currentBranch)
	headCommit, err := getCurrentHead()
	if err != nil {
		return fmt.Errorf("failed to get current head, error : %v", err)
	}

	fmt.Printf("Head currently pointing to %v\n", *headCommit)

	return nil
}

// add current files to be tracked
func addFilesToCommit(paths []string) error {
	return nil
}

// goes through the git history and prints every commit
func printLog() error {
	currentCommit, err := getCurrentHead()
	if err != nil {
		return err
	}
	for currentCommit != nil {
		commit, err := readCommit(*currentCommit)
		if err != nil {
			return err
		}
		fmt.Printf("Commit Hash\t: %v\n", *currentCommit)
		fmt.Println(commit)
		if len(commit.ParentCommit) > 0 {
			currentCommit = &commit.ParentCommit
		} else {
			break
		}
	}
	return nil
}

// reverts repo to the given commit hash
func switchToCommit(commitHash string) error {
	commit, err := readCommit(commitHash)
	if err != nil {
		return err
	}
	err = switchTreeToCommit("./", commit.TreeHash)
	if err != nil {
		return err
	}
	err = updateCurrentHead(commitHash)
	if err != nil {
		return err
	}
	return nil
}

// reads the given tree and compares it to current tree
func switchTreeToCommit(path string, treeHash string) error {
	// BFS
	tree, err := readTree(treeHash)
	if err != nil {
		return err
	}
	err = switchDirToCommit(path, tree)
	if err != nil {
		return err
	}
	// recursively revert all the subdirectories
	for _, file := range *tree {
		if file[0] == "tree" {
			err = switchTreeToCommit(filepath.Join(path, file[2]), file[1])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// given a list of files and a directory path, this resets the directory to the
// same state as given the list of files
func switchDirToCommit(path string, commitFiles *[][]string) error {
	dir, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	defer dir.Close()

	currentFiles, err := dir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}

	// find new files that need to be removed
	for _, f := range currentFiles {
		fullPath := filepath.Join(path, f.Name())
		if f.IsDir() {
			if slices.Contains[[]string](IGNORE_FOLDERS, f.Name()) {
				continue
			}
			found := false
			for _, old := range *commitFiles {
				if old[0] == "tree" && old[2] == f.Name() {
					found = true
					break
				}
			}
			if !found {
				err = os.RemoveAll(fullPath)
				if err != nil {
					return err
				}
				if DEBUG {
					fmt.Printf("Removed folder \"%v\"\n", fullPath)
				}
			}
		} else {
			found := false
			for _, old := range *commitFiles {
				if old[0] == "blob" && old[2] == f.Name() {
					found = true
					break
				}
			}
			if !found {
				err = os.Remove(fullPath)
				if err != nil {
					return err
				}
				if DEBUG {
					fmt.Printf("Removed file \"%v\"\n", fullPath)
				}
			}
		}
	}

	// go through all the old files / folders and extract them
	for _, file := range *commitFiles {
		newFileName := filepath.Join(path, file[2])
		if file[0] == "tree" {
			err := os.MkdirAll(newFileName, os.ModePerm)
			if err != nil {
				return fmt.Errorf("failed to extract old file \"%v\" (%v), error : %v\n", newFileName, file[1], err)
			}
			if DEBUG {
				fmt.Printf("Created folder \"%v\"\n", newFileName)
			}
		} else if file[0] == "blob" {
			newFile, err := os.OpenFile(newFileName, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
			if err != nil {
				return err
			}
			defer newFile.Close()

			f, err := os.Open(getObjectPath(file[1]))
			if err != nil {
				fmt.Printf("unable to read file: %v", err)
				return err
			}
			defer f.Close()
			reader := lzw.NewReader(f, lzw.LSB, 8)
			defer reader.Close()
			_, err = io.Copy(newFile, reader)
			if err != nil {
				return fmt.Errorf("failed to extract old file \"%v\" (%v), error : %v\n", newFileName, file[1], err)
			}

			if DEBUG {
				fmt.Printf("Updated file \"%v\"\n", newFileName)
			}
		}
	}

	return nil
}

// deserialize the given commit
func readCommit(commitHash string) (*Commit, error) {
	commitFileContent, err := readObjectFile(getObjectPath(commitHash))
	if err != nil {
		return nil, err
	}
	decoder := gob.NewDecoder(bytes.NewReader(*commitFileContent))
	commit := Commit{}
	if err := decoder.Decode(&commit); err != nil {
		return nil, fmt.Errorf("error decoding commit: %v", err)
	}
	return &commit, nil
}

// deserialize the given tree
func readTree(treeHash string) (*[][]string, error) {
	treeFileContent, err := readObjectFile(getObjectPath(treeHash))
	if err != nil {
		return nil, err
	}
	res := make([][]string, 0)
	treeContentString := string(*treeFileContent)
	lines := strings.Split(treeContentString, "\n")
	for _, line := range lines {
		elements := strings.Split(line, "\t")
		if len(elements) != 3 {
			continue
		}
		res = append(res, elements)
	}
	return &res, nil
}

// create and switch to a branch
func createAndSwitchBranch(branchName string) error {
	f, err := os.OpenFile(filepath.Join(BASEDIR, "refs", "heads", branchName), os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 5)
	if _, err := f.Read(buf); err != io.EOF {
		fmt.Printf("Branch \"%v\" already exists\n", branchName)
		return nil
	}

	commitHash, err := getCurrentHead()
	if err != nil {
		return err
	}
	f.WriteString(*commitHash)

	err = switchHead(branchName)
	if err != nil {
		return err
	}

	fmt.Printf("Created branch \"%v\" and switched to it.\n", branchName)

	return nil
}

// switch to given branch
func checkoutBranch(branchName string) error {
	// store the last to revert
	currentBranch, err := getCurrentBranch()
	if err != nil {
		return fmt.Errorf("failed to determine current branch, error : %v\n", currentBranch)
	}
	// first switch head then switch directory to commit
	err = switchHead(branchName)
	if err != nil {
		return err
	}

	headCommit, err := getCurrentHead()
	if err != nil {
		err = switchHead(*currentBranch)
		if err != nil {
			return err
		}
		return fmt.Errorf("failed to get current head, error : %v\n", err)
	}

	err = switchToCommit(*headCommit)
	if err != nil {
		err = switchHead(*currentBranch)
		if err != nil {
			return err
		}
		return fmt.Errorf("failed to switch to commit %v, error : %v\n", *headCommit, err)
	}

	return nil
}

func main() {
	debug := flag.Bool("debug", false, "enable debug mode")
	commitMessage := flag.String("message", "", "gogit --message \"commit message\" commit")
	objFilePath := flag.String("objPath", "", "gogit --objPath path/to/object/file read-obj-file")

	flag.Parse()

	if *debug {
		DEBUG = true
		fmt.Printf("debug mode enabled\n\n")
	}

	// Access positional argumentspositionalArgs
	positionalArgs := flag.Args()

	// Access a specific positional argument
	if len(positionalArgs) < 1 {
		fmt.Println("No command provided, please enter -h for help.")
	}

	if err := prepareDirs(); err != nil {
		fmt.Printf("Failed to create .git folders, err: %v\n", err)
		os.Exit(-1)
	}

	switch positionalArgs[0] {
	case "add":
		err := addFilesToCommit(positionalArgs[1:])
		if err != nil {
			log.Fatalf("failed to add files to commit, error : %v\n", err)
		}

	case "commit":
		treeHash, err := createTree(".")
		if err != nil {
			log.Fatalf("failed to created tree, error : %v", err)
		}
		currentHead, err := getCurrentHead()
		if err != nil {
			log.Fatalf("failed to get current head, error : %v\n", err)
		}
		commitHash, err := createCommit(*treeHash, *currentHead, *commitMessage)
		if err != nil {
			log.Fatalf("failed to create commit, error : %v\n", err)
		}
		fmt.Printf("Commit hash: %v\n", *commitHash)

	case "checkout":
		if len(positionalArgs) < 2 {
			log.Fatal("Please provide the branch name you want to switch to, using \"gogit checkout branch_name\"")
		}
		err := checkoutBranch(positionalArgs[1])
		if err != nil {
			log.Fatalf("failed to revert to commit, error : %v\n", err)
		}

	case "revert":
		if len(positionalArgs) < 2 {
			log.Fatal("Please provide the commit hash you want to revert to using \"gogit revert commit_hash\"")
		}
		err := switchToCommit(positionalArgs[1])
		if err != nil {
			log.Fatalf("failed to revert to commit, error : %v\n", err)
		}

	case "branch":
		if len(positionalArgs) < 2 {
			log.Fatal("Please provide the branch name you want to create using \"gogit branch commit_hash\"")
		}
		err := createAndSwitchBranch(positionalArgs[1])
		if err != nil {
			log.Fatalf("failed to revert to commit, error : %v\n", err)
		}

	case "log":
		err := printLog()
		if err != nil {
			log.Fatalf("failed to print log, error : %v\n", err)
		}

	case "read-obj-file":
		data, err := readObjectFile(*objFilePath)
		if err != nil {
			log.Fatalf("failed to read object file, error : %v\n", err)
		}
		fmt.Printf("Object file content:\n%v\n", string(*data))

	case "status":
		status()

	}
}
