package main

import (
	"regexp"
	"errors"
	"fmt"
	"log"
	"github.com/spf13/afero"
	"strings"
)

func RetrieveLatestSavepoint(dir string) (string, error) {
	if strings.HasSuffix(dir, "/") {
		dir = strings.TrimSuffix(dir, "/")
	}

	files, err := afero.ReadDir(filesystem, dir)
	if err != nil {
		return "", err
	}

	var newestFile string
	var newestTime int64 = 0
	for _, f := range files {
		filePath := dir + "/" + f.Name()
		fi, err := filesystem.Stat(filePath)
		if err != nil {
			return "", err
		}
		currTime := fi.ModTime().Unix()
		if currTime > newestTime {
			newestTime = currTime
			newestFile = filePath
		}
	}

	return newestFile, nil
}

func ExtractSavepointPath(output string) (string, error) {
	rgx := regexp.MustCompile("Savepoint completed. Path: file:(.*)\n")
	matches := rgx.FindAllStringSubmatch(output, -1)

	switch len(matches) {
	case 0:
		return "", errors.New("could not extract savepoint path from Flink's output")
	case 1:
		return matches[0][1], nil
	default:
		return "", errors.New("multiple matches for savepoint found")
	}
}

func CreateSavepoint(jobId string) (string, error) {
	out, err := Savepoint(jobId)
	if err != nil {
		return "", err
	}

	savepoint, err := ExtractSavepointPath(string(out))
	if err != nil {
		return "", err
	}

	if _, err = afero.Exists(filesystem, savepoint); err != nil {
		return "", err
	}

	return savepoint, nil
}

type UpdateJob struct {
	jobName                 string
	runArgs                 string
	localFilename           string
	remoteFilename          string
	apiToken								string
	jarArgs                 string
	savepointDirectory			string
	allowNonRestorableState bool
}

func (u UpdateJob) execute() ([]byte, error) {
	if len(u.jobName) == 0 {
		return nil, errors.New("unspecified argument 'jobName'")
	}

	log.Printf("starting job update for %v\n", u.jobName)

	jobIds, err := RetrieveRunningJobIds(u.jobName)
	if err != nil {
		log.Printf("Retrieving the running jobs failed: %v\n", err)
		return nil, err
	}

	deploy := Deploy{
		runArgs:                 u.runArgs,
		localFilename:           u.localFilename,
		remoteFilename:          u.remoteFilename,
		apiToken:								 u.apiToken,
		jarArgs:                 u.jarArgs,
		allowNonRestorableState: u.allowNonRestorableState,
	}
	switch len(jobIds) {
	case 0:
		log.Printf("No instance running for %v. Using last available savepoint\n", u.jobName)

		if len(u.savepointDirectory) == 0 {
			return nil, errors.New("cannot retrieve the latest savepoint without specifying the savepoint directory")
		}

		latestSavepoint, err := RetrieveLatestSavepoint(u.savepointDirectory)
		if err != nil {
			log.Printf("Retrieving the latest savepoint failed: %v\n", err)
			return nil, err
		}

		if len(latestSavepoint) != 0 {
			deploy.savepointPath = latestSavepoint
		}
	case 1:
		log.Printf("Found exactly 1 job named %v\n", u.jobName)
		jobId := jobIds[0]

		savepoint, err := CreateSavepoint(jobId)
		if err != nil {
			return nil, err
		}

		deploy.savepointPath = savepoint

		CancelJob(jobId)
	default:
		return nil, fmt.Errorf("%v has %v instances running", u.jobName, len(jobIds))
	}

	return deploy.execute()
}