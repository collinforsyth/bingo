// Copyright (c) Bartłomiej Płotka @bwplotka
// Licensed under the Apache License 2.0.

package gobin

import (
	"bytes"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	"golang.org/x/mod/modfile"
)

func readAllFileOrReader(modFile string, r io.Reader) (b []byte, err error) {
	if r != nil {
		return ioutil.ReadAll(r)
	}
	return ioutil.ReadFile(modFile)
}

// ModDirectPackage returns buildable package we encoded in the gobin controlled go module.
// We encode it as single direct module with end of line comment containing relative package path if any.
// If r is nil, modFile will be read.
func ModDirectPackage(modFile string, r io.Reader) (string, error) {
	b, err := readAllFileOrReader(modFile, r)
	if err != nil {
		return "", err
	}

	m, err := modfile.Parse(modFile, b, nil)
	if err != nil {
		return "", err
	}

	// We expect just one direct import.
	for _, r := range m.Require {
		if r.Indirect {
			continue
		}

		pkg := r.Mod.Path
		if len(r.Syntax.Suffix) > 0 {
			pkg = path.Join(pkg, r.Syntax.Suffix[0].Token[3:])
		}
		return pkg, nil
	}
	return "", nil
}

const metaComment = "// Auto generated by https://github.com/bwplotka/gobin. DO NOT EDIT"

// ModHasMeta returns true if given mod file contains metadata in comments we are adding in `AddMetaToMod`.
// If r is nil, modFile will be read.
func ModHasMeta(modFile string, r io.Reader) (bool, error) {
	b, err := readAllFileOrReader(modFile, r)
	if err != nil {
		return false, err
	}
	m, err := modfile.Parse(modFile, b, nil)
	if err != nil {
		return false, errors.Wrap(err, "failed to parse")
	}

	for _, c := range m.Module.Syntax.Comment().Suffix {
		if c.Token == metaComment {
			return true, nil
		}
	}
	return false, nil
}

// AddMeta comment on given module file to make sure users knows it's autogenerated.
// It also ensures that sub package path is recorded, which is required for package-level versioning.
func AddMetaToMod(modFile string, pkg string) (err error) {
	f, err := os.OpenFile(modFile, os.O_RDWR, os.ModePerm)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			if err != nil {
				err = errors.Wrapf(err, "additionally error on close: %v", cerr)
				return
			}
			err = cerr
		}
	}()
	b, err := ioutil.ReadAll(f)
	if err != nil {
		return err
	}

	has, err := ModHasMeta(modFile, bytes.NewReader(b))
	if err != nil {
		return err
	}
	if has {
		return errors.Errorf("module %s has already all meta", modFile)
	}

	m, err := modfile.Parse(modFile, b, nil)
	if err != nil {
		return errors.Wrap(err, "failed to parse")
	}

	// First meta.
	m.Module.Syntax.Suffix = append(m.Module.Syntax.Suffix, modfile.Comment{
		Suffix: true,
		Token:  metaComment,
	})

	for _, r := range m.Require {
		if r.Indirect {
			continue
		}

		// Add sub package info if needed.
		if r.Mod.Path != pkg {
			subPkg, err := filepath.Rel(r.Mod.Path, pkg)
			if err != nil {
				return err
			}
			r.Syntax.Suffix = append(r.Syntax.Suffix, modfile.Comment{
				Suffix: true,
				Token:  "// " + subPkg,
			})
		}

		// Save & Flush.
		newB, err := m.Format()
		if err != nil {
			return err
		}

		if err := f.Truncate(0); err != nil {
			return errors.Wrap(err, "truncate")
		}
		if _, err := f.Seek(0, 0); err != nil {
			return errors.Wrap(err, "seek")
		}

		_, err = f.Write(newB)
		return err
	}
	return errors.Errorf("empty module found in %s", modFile)
}
