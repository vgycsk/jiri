// Copyright 2015 The Vanadium Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profiles

import (
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"v.io/jiri/lib/tool"
)

const (
	defaultFileMode = os.FileMode(0644)
)

// Profile represents a suite of software that is managed by an implementation
// of profiles.Manager.
type Profile struct {
	Name    string
	Root    string
	Targets []*Target
}

type profilesSchema struct {
	XMLName  xml.Name         `xml:"profiles"`
	Profiles []*profileSchema `xml:"profile"`
}

type profileSchema struct {
	XMLName xml.Name        `xml:"profile"`
	Name    string          `xml:"name,attr"`
	Root    string          `xml:"root,attr"`
	Targets []*targetSchema `xml:"target"`
}

type targetSchema struct {
	XMLName         xml.Name    `xml:"target"`
	Tag             string      `xml:"tag,attr"`
	Arch            string      `xml:"arch,attr"`
	OS              string      `xml:"os,attr"`
	InstallationDir string      `xml:"installation-directory,attr"`
	Version         string      `xml:"version,attr"`
	UpdateTime      time.Time   `xml:"date,attr"`
	Env             Environment `xml:"envvars"`
}

type profileDB struct {
	sync.Mutex
	db map[string]*Profile
}

func newDB() *profileDB {
	return &profileDB{db: make(map[string]*Profile)}
}

var (
	db = newDB()
)

// Profiles returns the names, in lexicographic order, of all of the currently
// available profiles as read or stored in the manifest. A profile name may
// be used to lookup a profile manager or the current state of a profile.
func Profiles() []string {
	return db.profiles()
}

// LookupProfile returns the profile for the name profile or nil if one is
// not found.
func LookupProfile(name string) *Profile {
	return db.profile(name)
}

// LookupProfileTarget returns the target information stored for the name
// profile. Typically, the target parameter will contain just the Tag field.
func LookupProfileTarget(name string, target Target) *Target {
	mgr := db.profile(name)
	if mgr == nil {
		return nil
	}
	return FindTarget(mgr.Targets, &target)
}

// InstallProfile will create a new profile and store in the profiles manifest,
// it has no effect if the profile already exists.
func InstallProfile(name, root string) {
	db.installProfile(name, root)
}

// AddProfileTarget adds the specified target to the named profile.
// The UpdateTime of the newly installed target will be set to time.Now()
func AddProfileTarget(name string, target Target) error {
	return db.addProfileTarget(name, &target)
}

// RemoveProfileTarget removes the specified target from the named profile.
// If this is the last target for the profile then the profile will be deleted
// from the manifest. It returns true if the profile was so deleted or did
// not originally exist.
func RemoveProfileTarget(name string, target Target) bool {
	return db.removeProfileTarget(name, &target)
}

// UpdateProfileTarget updates the specified target from the named profile.
// The UpdateTime of the updated target will be set to time.Now()
func UpdateProfileTarget(name string, target Target) {
	db.updateProfileTarget(name, &target)
}

// HasTarget returns true if the named profile exists and has the specified
// target already installed.
func HasTarget(name string, target Target) bool {
	profile := db.profile(name)
	if profile == nil {
		return false
	}
	return FindTarget(profile.Targets, &target) != nil
}

// HasTargetTsg returns true if the named profile exists and has the specified
// target tag already installed.
func HasTargetTag(name string, target Target) bool {
	profile := db.profile(name)
	if profile == nil {
		return false
	}
	return FindTargetByTag(profile.Targets, &target) != nil
}

// Read reads the specified manifest file to obtain the current set of
// installed profiles.
func Read(ctx *tool.Context, filename string) error {
	return db.read(ctx, filename)
}

// Write writes the current set of installed profiles to the specified manifest
// file.
func Write(ctx *tool.Context, filename string) error {
	return db.write(ctx, filename)
}

func (pdb *profileDB) installProfile(name, root string) {
	pdb.Lock()
	defer pdb.Unlock()
	if p := pdb.db[name]; p == nil {
		pdb.db[name] = &Profile{Name: name, Root: root}
	}
}

func (pdb *profileDB) addProfileTarget(name string, target *Target) error {
	pdb.Lock()
	defer pdb.Unlock()
	target.UpdateTime = time.Now()
	if pi, present := pdb.db[name]; present {
		if existing := FindTargetByTag(pi.Targets, target); existing != nil {
			return fmt.Errorf("tag %q is already used by %s", target.Tag, existing)
		}
		pi.Targets = AddTarget(pi.Targets, target)
		return nil
	}
	pdb.db[name] = &Profile{Name: name}
	pdb.db[name].Targets = AddTarget(nil, target)
	return nil
}

func (pdb *profileDB) updateProfileTarget(name string, target *Target) {
	pdb.Lock()
	defer pdb.Unlock()
	target.UpdateTime = time.Now()
	pi, present := pdb.db[name]
	if !present {
		return
	}
	tg := FindTarget(pi.Targets, target)
	if tg != nil {
		tg.UpdateTime = time.Now()
		tg.Version = target.Version
	}
}

func (pdb *profileDB) removeProfileTarget(name string, target *Target) bool {
	pdb.Lock()
	defer pdb.Unlock()

	pi, present := pdb.db[name]
	if !present {
		return true
	}
	pi.Targets = RemoveTarget(pi.Targets, target)
	if len(pi.Targets) == 0 {
		delete(pdb.db, name)
		return true
	}
	return false
}

func (pdb *profileDB) profiles() []string {
	pdb.Lock()
	defer pdb.Unlock()
	return pdb.profilesUnlocked()

}

func (pdb *profileDB) profilesUnlocked() []string {
	names := make([]string, 0, len(pdb.db))
	for name := range pdb.db {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (pdb *profileDB) profile(name string) *Profile {
	pdb.Lock()
	defer pdb.Unlock()
	return pdb.db[name]
}

func (pdb *profileDB) read(ctx *tool.Context, filename string) error {
	pdb.Lock()
	defer pdb.Unlock()

	data, err := ctx.Run().ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var schema profilesSchema
	if err := xml.Unmarshal(data, &schema); err != nil {
		return fmt.Errorf("Unmarshal(%v) failed: %v", string(data), err)
	}
	for _, profile := range schema.Profiles {
		name := profile.Name
		pdb.db[name] = &Profile{
			Name: name,
			Root: profile.Root,
		}
		for _, target := range profile.Targets {
			pdb.db[name].Targets = append(pdb.db[name].Targets, &Target{
				Tag:             target.Tag,
				Arch:            target.Arch,
				OS:              target.OS,
				Env:             target.Env,
				Version:         target.Version,
				UpdateTime:      target.UpdateTime,
				InstallationDir: target.InstallationDir,
				isSet:           true,
			})
		}
	}
	return nil
}

func (pdb *profileDB) write(ctx *tool.Context, filename string) error {
	pdb.Lock()
	defer pdb.Unlock()

	var schema profilesSchema
	for i, name := range pdb.profilesUnlocked() {
		profile := pdb.db[name]
		schema.Profiles = append(schema.Profiles, &profileSchema{
			Name: name,
			Root: profile.Root,
		})
		for _, target := range profile.Targets {
			sort.Strings(target.Env.Vars)
			schema.Profiles[i].Targets = append(schema.Profiles[i].Targets,
				&targetSchema{
					Tag:             target.Tag,
					Arch:            target.Arch,
					OS:              target.OS,
					Env:             target.Env,
					Version:         target.Version,
					InstallationDir: target.InstallationDir,
					UpdateTime:      target.UpdateTime,
				})
		}
	}

	data, err := xml.MarshalIndent(schema, "", "  ")
	if err != nil {
		return fmt.Errorf("MarshalIndent() failed: %v", err)
	}

	oldName := filename + ".prev"
	newName := filename + fmt.Sprintf(".%d", time.Now().UnixNano())

	if err := ctx.Run().WriteFile(newName, data, defaultFileMode); err != nil {
		return err
	}

	if FileExists(ctx, filename) {
		if err := ctx.Run().Rename(filename, oldName); err != nil {
			return err
		}
	}

	if err := ctx.Run().Rename(newName, filename); err != nil {
		return err
	}

	return nil
}
