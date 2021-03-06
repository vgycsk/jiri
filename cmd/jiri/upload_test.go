// Copyright 2016 The Fuchsia Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fuchsia.googlesource.com/jiri"
	"fuchsia.googlesource.com/jiri/gerrit"
	"fuchsia.googlesource.com/jiri/gitutil"
	"fuchsia.googlesource.com/jiri/jiritest"
	"fuchsia.googlesource.com/jiri/project"
	"fuchsia.googlesource.com/jiri/runutil"
)

func projectName(i int) string {
	return fmt.Sprintf("project-%d", i)
}

// setupUniverse creates a fake jiri root with 3 remote projects.  Each project
// has a README with text "initial readme".
func setupUniverse(t *testing.T) ([]project.Project, *jiritest.FakeJiriRoot, func()) {
	fake, cleanup := jiritest.NewFakeJiriRoot(t)
	success := false
	defer func() {
		if !success {
			cleanup()
		}
	}()

	// Create some projects and add them to the remote manifest.
	numProjects := 3
	localProjects := []project.Project{}
	for i := 0; i < numProjects; i++ {
		name := projectName(i)
		path := fmt.Sprintf("path-%d", i)
		if err := fake.CreateRemoteProject(name); err != nil {
			t.Fatal(err)
		}
		p := project.Project{
			Name:   name,
			Path:   filepath.Join(fake.X.Root, path),
			Remote: fake.Projects[name],
		}
		localProjects = append(localProjects, p)
		if err := fake.AddProject(p); err != nil {
			t.Fatal(err)
		}
	}

	// Create initial commit in each repo.
	for _, remoteProjectDir := range fake.Projects {
		writeReadme(t, fake.X, remoteProjectDir, "initial readme")
	}

	success = true
	return localProjects, fake, cleanup
}

// setupTest creates a setup for testing the upload tool.
func setupUploadTest(t *testing.T) (*jiritest.FakeJiriRoot, []project.Project, func()) {
	localProjects, fake, cleanup := setupUniverse(t)
	if err := fake.UpdateUniverse(false); err != nil {
		t.Fatal(err)
	}
	return fake, localProjects, cleanup
}

func assertUploadPushedFilesToRef(t *testing.T, jirix *jiri.X, gerritPath, pushedRef string, files []string) {
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(gerritPath); err != nil {
		t.Fatal(err)
	}
	if err := gitutil.New(jirix).CheckoutBranch(pushedRef); err != nil {
		t.Fatal(err)
	}
	assertFilesCommitted(t, jirix, files)
}

func resetFlags() {
	uploadCcsFlag = ""
	uploadHostFlag = ""
	uploadPresubmitFlag = string(gerrit.PresubmitTestTypeAll)
	uploadReviewersFlag = ""
	uploadTopicFlag = ""
	uploadVerifyFlag = true
	uploadRebaseFlag = false
	uploadMultipartFlag = false
	uploadBranchFlag = ""
	uploadRemoteBranchFlag = ""
}

func TestUpload(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	branch := "my-branch"
	git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateBranchWithUpstream(branch, "origin/master"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1"}
	commitFiles(t, fake.X, files)

	gerritPath := fake.Projects[localProjects[1].Name]
	uploadHostFlag = gerritPath
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef := "refs/for/master%topic=" + topic
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)

	uploadRemoteBranchFlag = "new-branch"
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef = fmt.Sprintf("refs/for/%s%%topic=%s", uploadRemoteBranchFlag, topic)

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)
}

func TestUploadMultipart(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	branch := "my-branch"
	for i := 0; i < 2; i++ {
		if err := os.Chdir(localProjects[i].Path); err != nil {
			t.Fatal(err)
		}
		git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
		if err := git.CreateBranchWithUpstream(branch, "origin/master"); err != nil {
			t.Fatal(err)
		}
		if err := git.CheckoutBranch(branch); err != nil {
			t.Fatal(err)
		}
		files := []string{"file-1" + strconv.Itoa(i)}
		commitFiles(t, fake.X, files)
		files = []string{"file-2" + strconv.Itoa(i)}
		commitFiles(t, fake.X, files)
	}

	gerritPath := fake.Projects[localProjects[0].Name]
	uploadHostFlag = gerritPath
	uploadMultipartFlag = true
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef := "refs/for/master%topic=" + topic

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})

	uploadRemoteBranchFlag = "new-branch"
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef = fmt.Sprintf("refs/for/%s%%topic=%s", uploadRemoteBranchFlag, topic)

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})
}

func TestUploadMultipartWithBranchFlagSimple(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	branch := "my-branch"
	for i := 0; i < 2; i++ {
		if err := os.Chdir(localProjects[i].Path); err != nil {
			t.Fatal(err)
		}
		git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
		if err := git.CreateBranchWithUpstream(branch, "origin/master"); err != nil {
			t.Fatalf("%v", err)
		}
		if err := git.CheckoutBranch(branch); err != nil {
			t.Fatalf("%v", err)
		}
		files := []string{"file-1" + strconv.Itoa(i)}
		commitFiles(t, fake.X, files)
		files = []string{"file-2" + strconv.Itoa(i)}
		commitFiles(t, fake.X, files)
	}
	if err := os.Chdir(fake.X.Root); err != nil {
		t.Fatal(err)
	}

	gerritPath := fake.Projects[localProjects[0].Name]
	uploadHostFlag = gerritPath
	uploadMultipartFlag = true
	uploadBranchFlag = branch
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef := "refs/for/master%topic=" + topic

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, []string{"file-10", "file-20"})
}

func TestUploadRebase(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	branch := "my-branch"
	git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.Config("user.email", "john.doe@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := git.Config("user.name", "John Doe"); err != nil {
		t.Fatal(err)
	}
	if err := git.CreateBranchWithUpstream(branch, "origin/master"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	localFiles := []string{"file1"}
	commitFiles(t, fake.X, localFiles)

	if err := os.Chdir(fake.Projects[localProjects[1].Name]); err != nil {
		t.Fatal(err)
	}
	remoteFiles := []string{"file2"}
	commitFiles(t, fake.X, remoteFiles)

	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}

	gerritPath := fake.Projects[localProjects[1].Name]
	uploadHostFlag = gerritPath
	uploadRebaseFlag = true
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef := "refs/for/master%topic=" + topic
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, localFiles)
	assertUploadPushedFilesToRef(t, fake.X, localProjects[1].Path, branch, remoteFiles)
}

func TestUploadMultipleCommits(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	branch := "my-branch"
	git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateBranchWithUpstream(branch, "origin/master"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files1 := []string{"file1"}
	commitFiles(t, fake.X, files1)

	files2 := []string{"file2"}
	commitFiles(t, fake.X, files2)

	gerritPath := fake.Projects[localProjects[1].Name]
	uploadHostFlag = gerritPath
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}

	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef := "refs/for/master%topic=" + topic
	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, append(files1, files2...))
}

func TestUploadThrowsErrorWhenNotOnBranch(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CheckoutBranch("HEAD", gitutil.DetachOpt(true)); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1"}
	commitFiles(t, fake.X, files)

	if err := runUpload(fake.X, []string{}); err == nil {
		t.Fatalf("Should have got a error here.")
	} else if !strings.Contains(err.Error(), "project is not on any branch") {
		t.Fatalf("Wrong error: %s", err)
	}
}

func TestUploadFailsWhenNoGerritHost(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	branch := "my-branch"
	git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateBranchWithUpstream(branch, "origin/master"); err != nil {
		t.Fatal(err)
	}
	if err := git.CheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1"}
	commitFiles(t, fake.X, files)

	if err := runUpload(fake.X, []string{}); err == nil {
		t.Fatalf("Should have got a error here.")
	} else if !strings.Contains(err.Error(), "Please use the '--host' flag") {
		t.Fatalf("Wrong error: %s", err)
	}
}

func TestUploadUntrackedBranch(t *testing.T) {
	defer resetFlags()
	fake, localProjects, cleanup := setupUploadTest(t)
	defer cleanup()
	currentDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(currentDir); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(localProjects[1].Path); err != nil {
		t.Fatal(err)
	}
	branch := "my-branch"
	git := gitutil.New(fake.X, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com"))
	if err := git.CreateAndCheckoutBranch(branch); err != nil {
		t.Fatal(err)
	}
	files := []string{"file1"}
	commitFiles(t, fake.X, files)

	gerritPath := fake.Projects[localProjects[1].Name]
	uploadHostFlag = gerritPath
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	topic := fmt.Sprintf("%s-%s", os.Getenv("USER"), branch)
	expectedRef := "refs/for/master%topic=" + topic

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)

	uploadRemoteBranchFlag = "new-branch"
	if err := runUpload(fake.X, []string{}); err != nil {
		t.Fatal(err)
	}
	expectedRef = fmt.Sprintf("refs/for/%s%%topic=%s", uploadRemoteBranchFlag, topic)

	assertUploadPushedFilesToRef(t, fake.X, gerritPath, expectedRef, files)
}

// commitFile commits a file with the specified content into a branch
func commitFile(t *testing.T, jirix *jiri.X, filename string, content string) {
	s := jirix.NewSeq()
	if err := s.WriteFile(filename, []byte(content), 0644).Done(); err != nil {
		t.Fatalf("%v", err)
	}
	commitMessage := "Commit " + filename
	if err := gitutil.New(jirix, gitutil.UserNameOpt("John Doe"), gitutil.UserEmailOpt("john.doe@example.com")).CommitFile(filename, commitMessage); err != nil {
		t.Fatalf("%v", err)
	}
}

// commitFiles commits the given files into to current branch.
func commitFiles(t *testing.T, jirix *jiri.X, filenames []string) {
	// Create and commit the files one at a time.
	for _, filename := range filenames {
		content := "This is file " + filename
		commitFile(t, jirix, filename, content)
	}
}

// assertFilesCommitted asserts that the files exist and are committed
// in the current branch.
func assertFilesCommitted(t *testing.T, jirix *jiri.X, files []string) {
	assertFilesExist(t, jirix, files)
	for _, file := range files {
		if !gitutil.New(jirix).IsFileCommitted(file) {
			t.Fatalf("expected file %v to be committed but it is not", file)
		}
	}
}

// assertFilesExist asserts that the files exist.
func assertFilesExist(t *testing.T, jirix *jiri.X, files []string) {
	s := jirix.NewSeq()
	for _, file := range files {
		if _, err := s.Stat(file); err != nil {
			if runutil.IsNotExist(err) {
				t.Fatalf("expected file %v to exist but it did not", file)
			}
			t.Fatalf("%v", err)
		}
	}
}
