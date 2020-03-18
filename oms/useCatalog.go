// Copyright (c) 2016 OpenM++
// This code is licensed under the MIT license (see LICENSE.txt for details)

package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/language"

	"github.com/openmpp/go/ompp/db"
	"github.com/openmpp/go/ompp/omppLog"
)

// RefreshSqlite open db-connection to model.sqlite files in model directory and read model_dic row for each model.
// If multiple version of the same model (equal by digest) exist in different files then only one is used.
// All previously opened db connections are closed.
func (mc *ModelCatalog) RefreshSqlite(modelDir, modelLogDir string) error {

	// model directory must exist
	isDir := modelDir != "" && modelDir != "."
	if isDir {
		isDir = isDirExist(modelDir) == nil
	}
	if !isDir {
		return errors.New("Error: model directory not exist or not accesible: " + modelDir)
	}

	// model log directory is optional, if empty or not exists then model log disabled
	isLogDir := modelLogDir != "" && modelLogDir != "."
	if isLogDir {
		isLogDir = isDirExist(modelLogDir) == nil
	}

	// get list of model/dir/*.sqlite files
	pathLst := []string{}
	err := filepath.Walk(modelDir, func(src string, info os.FileInfo, err error) error {
		if err != nil {
			if err != filepath.SkipDir {
				omppLog.Log("Error at refresh model catalog, path: ", src, " : ", err.Error())
			}
			return err
		}
		if strings.EqualFold(filepath.Ext(src), ".sqlite") {
			pathLst = append(pathLst, src)
		}
		return nil
	})
	if err != nil {
		omppLog.Log("Error: fail to list model directory: ", err.Error())
		return errors.New("Error: fail to list model directory")
	}
	sort.Strings(pathLst) // sort by path to model.sqlite: same as sort by model name in default case

	// make list of models from model.sqlite files:
	// open db connection to model.sqlite and read list of model_dic rows.
	// if model exist in multiple sqlite files then only one is used.
	var mLst []modelDef
	for _, fp := range pathLst {

		// open db connection and check version of openM++ database
		dbc, _, err := db.Open(db.MakeSqliteDefault(fp), db.SQLiteDbDriver, false)
		if err != nil {
			omppLog.Log("Error: ", fp, " : ", err.Error())
			continue
		}
		nv, err := db.OpenmppSchemaVersion(dbc)
		if err != nil || nv < db.MinSchemaVersion {
			omppLog.Log("Error: invalid database, likely not an openM++ database", fp)
			dbc.Close()
		}
		d := filepath.Dir(fp)

		// read list of models: model_dic rows
		dicLst, err := db.GetModelList(dbc)
		if err != nil || len(dicLst) <= 0 {
			omppLog.Log("Warning: empty database, no models found: ", fp)
			dbc.Close()
			continue // skip this database
		}

		ls, err := db.GetLanguages(dbc)
		if err != nil || ls == nil {
			omppLog.Log("Warning: no languages found in database: ", fp)
			dbc.Close()
			continue // skip this database
		}

		// append to list of models if not already exist
	dicLoop:
		for idx := range dicLst {

			// skip model if same digest already exist in model list
			for k := range mLst {
				if dicLst[idx].Digest == mLst[k].meta.Model.Digest {
					omppLog.Log("Skip: model already exist in other database: ", dicLst[idx].Name, " ", dicLst[idx].Digest)
					continue dicLoop
				}
			}

			// make model languages list, starting from default language
			ml := []string{}
			lt := []language.Tag{}

			for k := range ls.Lang {
				if ls.Lang[k].LangCode == dicLst[idx].DefaultLangCode {
					ml = append([]string{ls.Lang[k].LangCode}, ml...)
					lt = append([]language.Tag{language.Make(ls.Lang[k].LangCode)}, lt...)
				} else {
					ml = append(ml, ls.Lang[k].LangCode)
					lt = append(lt, language.Make(ls.Lang[k].LangCode))
				}
			}

			// append to model list
			mLst = append(mLst, modelDef{
				dbConn:     dbc,
				binDir:     d,
				logDir:     modelLogDir,
				isLogDir:   isLogDir,
				isMetaFull: false,
				meta:       &db.ModelMeta{Model: dicLst[idx]},
				langCodes:  ml,
				langMeta:   ls,
				matcher:    language.NewMatcher(lt)})
		}
	}

	// lock and update model catalog
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	// update model directories
	mc.modelDir = modelDir
	mc.isDirEnabled = isDir
	mc.modelLogDir = modelLogDir
	mc.isLogDirEnabled = isLogDir

	// close existing connections and store updated list of models and db connections
	for k := range mc.modelLst {
		if err := mc.modelLst[k].dbConn.Close(); err != nil {
			omppLog.Log("Error: close db connection error: " + err.Error())
		}
	}

	mc.modelLst = mLst // set new list of the models
	return nil
}

// Close all db-connection to model.sqlite files and clear model list.
func (mc *ModelCatalog) Close() error {

	// lock and update model catalog
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	// close existing db connections
	var firstErr error
	for k := range mc.modelLst {
		if err := mc.modelLst[k].dbConn.Close(); err != nil {
			omppLog.Log("Error: close db connection error: " + err.Error())
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	// clear model list
	mc.modelLst = []modelDef{}
	return firstErr
}

// get "public" configuration of model catalog
func (mc *ModelCatalog) toPublicConfig() *ModelCatalogConfig {

	// lock run catalog and return results
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	mp := ModelCatalogConfig{
		ModelDir:        mc.modelDir,
		ModelLogDir:     mc.modelLogDir,
		IsLogDirEnabled: mc.isLogDirEnabled,
	}
	return &mp
}

// getModelDir return model directory
func (mc *ModelCatalog) getModelDir() (string, bool) {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()
	return mc.modelDir, mc.isDirEnabled
}

// getModelLogDir return default model directory and true if that directory exist
func (mc *ModelCatalog) getModelLogDir() (string, bool) {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()
	return mc.modelLogDir, mc.isLogDirEnabled
}

// indexByDigest return index of model by digest.
// It can be used only inside of lock.
func (mc *ModelCatalog) indexByDigest(digest string) (int, bool) {
	for k := range mc.modelLst {
		if mc.modelLst[k].meta.Model.Digest == digest {
			return k, true
		}
	}
	return 0, false
}

// indexByDigestOrName return index of model by digest or by name.
// It can be used only inside of lock.
// If digest exist in model list then return index by digest else first index of name.
func (mc *ModelCatalog) indexByDigestOrName(dn string) (int, bool) {
	n := -1
	for k := range mc.modelLst {
		if mc.modelLst[k].meta.Model.Digest == dn {
			return k, true // return: digest found
		}
		if n < 0 && mc.modelLst[k].meta.Model.Name == dn {
			n = k
		}
	}
	if n >= 0 {
		return n, true // return: name found
	}
	return 0, false // not found
}

/*
// binDirectoryByDigest return model bin directory where model.exe expected to be located.
func (mc *ModelCatalog) binDirectoryByDigest(digest string) (string, bool) {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	idx, ok := mc.indexByDigest(digest)
	if !ok {
		return "", false // model not found, empty result
	}

	return mc.modelLst[idx].binDir, true
}

// logDirectoryByDigest return model log directory and log enabled/disabled flag by model digest.
func (mc *ModelCatalog) logDirectoryByDigest(digest string) (string, bool) {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	idx, ok := mc.indexByDigest(digest)
	if !ok {
		return "", false // model not found, empty result
	}

	return mc.modelLst[idx].logDir, mc.modelLst[idx].isLogDir
}
*/

// AllModelDigests return digests for all models.
func (mc *ModelCatalog) allModelDigests() []string {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	ds := make([]string, len(mc.modelLst))
	for idx := range mc.modelLst {
		ds[idx] = mc.modelLst[idx].meta.Model.Digest
	}
	return ds
}

// binDirectoryByDigest return model bin directory where model.exe expected to be located.
func (mc *ModelCatalog) modelBasicByDigest(digest string) (modelBasic, bool) {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	idx, ok := mc.indexByDigest(digest)
	if !ok {
		return modelBasic{}, false // model not found, empty result
	}
	return modelBasic{
			name:     mc.modelLst[idx].meta.Model.Name,
			digest:   mc.modelLst[idx].meta.Model.Digest,
			binDir:   mc.modelLst[idx].binDir,
			logDir:   mc.modelLst[idx].logDir,
			isLogDir: mc.modelLst[idx].isLogDir,
		},
		true
}

// AllModelBasics return basic info about all models: name, digest, files location.
func (mc *ModelCatalog) allModels() []modelBasic {
	mc.theLock.Lock()
	defer mc.theLock.Unlock()

	mbs := make([]modelBasic, len(mc.modelLst))
	for idx := range mc.modelLst {
		mbs[idx] = modelBasic{
			name:     mc.modelLst[idx].meta.Model.Name,
			digest:   mc.modelLst[idx].meta.Model.Digest,
			binDir:   mc.modelLst[idx].binDir,
			logDir:   mc.modelLst[idx].logDir,
			isLogDir: mc.modelLst[idx].isLogDir,
		}
	}
	return mbs
}
