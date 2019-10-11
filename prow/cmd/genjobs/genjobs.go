// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	flag "github.com/spf13/pflag"
	"sigs.k8s.io/yaml"

	"k8s.io/apimachinery/pkg/util/sets"
	prowjob "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"k8s.io/test-infra/prow/config"
)

const (
	autogenHeader     = "# THIS FILE IS AUTOGENERATED. DO NOT EDIT. See prow/cmd/genjobs/README.md\n"
	modifier          = "private"
	filenameSeparator = "."
	jobnameSeparator  = "_"
	yamlExt           = ".(yml|yaml)$"
)

// Command-line flags

var bucket string
var cluster string
var clean bool
var sshKeySecret string
var labels map[string]string
var branches []string
var input string
var output string
var repoWhitelist sets.String
var repoBlacklist sets.String
var jobWhitelist sets.String
var jobBlacklist sets.String
var orgMap map[string]string

// validateOrgRepo validates that the org and repo for a job pass validation and should be converted.
func validateOrgRepo(org string, repo string) bool {
	_, hasOrg := orgMap[org]

	if !hasOrg || repoBlacklist.Has(repo) || (len(repoWhitelist) > 0 && !repoWhitelist.Has(repo)) {
		return false
	}

	return true
}

// validateJob validates that the job passes validation and should be converted.
func validateJob(name string, patterns []string) bool {
	if jobBlacklist.Has(name) || (len(jobWhitelist) > 0 && !jobWhitelist.Has(name)) || !isMatchBranch(patterns) {
		return false
	}

	return true
}

// validateFlags validates the command-line flags.
func validateFlags() {
	var err error

	if len(orgMap) == 0 {
		printErrAndExit("-m, --mapping option is required.", 1)
	}

	input, err = filepath.Abs(input)
	if err != nil {
		printErrAndExit(fmt.Sprintf("-i, --input option invalid: %v.", input), 1)
	}

	output, err = filepath.Abs(output)
	if err != nil {
		printErrAndExit(fmt.Sprintf("-o, --output option invalid: %v.", output), 1)
	}
}

// printErr prints error to stderr.
func printErr(msg string) {
	_, _ = fmt.Fprintln(os.Stderr, msg)
}

// printErrAndExit prints error to stderr and exits with te provided code.
func printErrAndExit(msg string, code int) {
	printErr(msg)
	os.Exit(code)
}

// isMatchBranch validates that the branch for a job passes validation and should be converted.
func isMatchBranch(patterns []string) bool {
	if len(branches) == 0 {
		return true
	}

	for _, branch := range branches {
		for _, pattern := range patterns {
			if regexp.MustCompile(pattern).MatchString(branch) {
				return true
			}
		}
	}

	return false
}

// renameFile renames a file based on a specified regular expression pattern.
func renameFile(pat string, src string, repl string) string {
	return regexp.MustCompile(pat).ReplaceAllString(src, repl)
}

// convertOrgRepoStr translates the provided job org and repo based on the specified org mapping.
func convertOrgRepoStr(s string) string {
	a := strings.Split(s, "/")
	org, repo := a[0], a[1]

	valid := validateOrgRepo(org, repo)

	if !valid {
		return ""
	}

	return strings.Join([]string{orgMap[org], repo}, "/")
}

// updateUtilityConfig updates the jobs UtilityConfig fields based on provided inputs to work with private repositories.
func updateUtilityConfig(job *config.UtilityConfig) {
	if job.DecorationConfig == nil {
		job.DecorationConfig = &prowjob.DecorationConfig{
			GCSConfiguration: &prowjob.GCSConfiguration{
				Bucket: bucket,
			},
			SSHKeySecrets: []string{sshKeySecret},
		}
	} else {
		if job.DecorationConfig.GCSConfiguration == nil {
			job.DecorationConfig.GCSConfiguration = &prowjob.GCSConfiguration{
				Bucket: bucket,
			}
		} else {
			job.DecorationConfig.GCSConfiguration.Bucket = bucket
		}
		if job.DecorationConfig.SSHKeySecrets == nil {
			job.DecorationConfig.SSHKeySecrets = []string{sshKeySecret}
		} else {
			job.DecorationConfig.SSHKeySecrets = append(job.DecorationConfig.SSHKeySecrets, sshKeySecret)
		}
	}
}

// updateJobBase updates the jobs JobBase fields based on provided inputs to work with private repositories.
func updateJobBase(job *config.JobBase, orgrepo string) {
	job.Name = job.Name + jobnameSeparator + modifier
	job.CloneURI = fmt.Sprintf("git@github.com:%s.git", orgrepo)
	job.Annotations = nil

	if cluster != "" && cluster != "default" {
		job.Cluster = cluster
	}

	if job.Labels == nil {
		job.Labels = make(map[string]string)
	}

	for labelK, labelV := range labels {
		job.Labels[labelK] = labelV
	}
}

// getOutPath derives the output path from the specified input directory and current path.
func getOutPath(p string, in string) string {
	segments := strings.FieldsFunc(strings.TrimPrefix(p, in), func(c rune) bool { return c == '/' })

	var org string

	var repo string

	var file string

	if len(segments) >= 3 {
		org = segments[len(segments)-3]
		repo = segments[len(segments)-2]
		file = segments[len(segments)-1]

		if newOrg, ok := orgMap[org]; ok {
			return filepath.Join(output, newOrg, repo, renameFile(`\b`+org+`\b`, file, newOrg))
		}
	} else if len(segments) == 2 {
		org = segments[len(segments)-2]
		file = segments[len(segments)-1]

		if newOrg, ok := orgMap[org]; ok {
			return filepath.Join(output, newOrg, renameFile(`\b`+org+`\b`, file, newOrg))
		}
	} else if len(segments) == 1 {
		file = segments[len(segments)-1]

		if !strings.HasPrefix(file, modifier) {
			return filepath.Join(output, modifier+filenameSeparator+file)
		}
	}

	return ""
}

// cleanOutPath deletes all files as the specified output path.
func cleanOutPath(p string) {
	for _, org := range orgMap {
		p = filepath.Join(p, org)

		err := os.RemoveAll(p)
		if err != nil {
			printErr(fmt.Sprintf("unable to clean directory %v: %v.", p, err))
		}
	}
}

// writeOutFile writes presubmit and postsubmit jobs definitions to the designated output path.
func writeOutFile(p string, pre map[string][]config.Presubmit, post map[string][]config.Postsubmit) {
	if len(pre) == 0 && len(post) == 0 {
		return
	}

	jobConfig := config.JobConfig{}

	err := jobConfig.SetPresubmits(pre)
	if err != nil {
		printErr(fmt.Sprintf("unable to set presubmits for path %v: %v.", p, err))
	}

	err = jobConfig.SetPostsubmits(post)
	if err != nil {
		printErr(fmt.Sprintf("unable to set postsubmits for path %v: %v.", p, err))
	}

	jobConfigYaml, err := yaml.Marshal(jobConfig)
	if err != nil {
		printErr(fmt.Sprintf("unable to marshal job config output directory: %v.", err))
		return
	}

	outBytes := []byte(autogenHeader)
	outBytes = append(outBytes, jobConfigYaml...)

	dir := filepath.Dir(p)

	err = os.MkdirAll(dir, os.ModePerm)
	if err != nil {
		printErr(fmt.Sprintf("unable to create output directory %v: %v.", dir, err))
	}

	err = ioutil.WriteFile(p, outBytes, 0644)
	if err != nil {
		printErr(fmt.Sprintf("unable to write jobs to path %v: %v.", p, err))
	}
}

// walkTree is the walk function which handles job conversion per yaml definition file.
func walkTree(p string, info os.FileInfo, err error) error {
	if err != nil {
		return nil
	}

	absPath, _ := filepath.Abs(p)

	if !regexp.MustCompile(yamlExt).MatchString(filepath.Ext(absPath)) {
		return nil
	}

	outPath := getOutPath(absPath, input)
	if outPath == "" {
		return nil
	}

	jobs, err := config.ReadJobConfig(absPath)
	if err != nil {
		return nil
	}

	presubmit := make(map[string][]config.Presubmit)
	postsubmit := make(map[string][]config.Postsubmit)

	// Presubmits
	for orgrepo, pre := range jobs.Presubmits {
		orgrepo = convertOrgRepoStr(orgrepo)
		if orgrepo == "" {
			continue
		}

		for _, job := range pre {
			valid := validateJob(job.Name, job.Branches)
			if !valid {
				continue
			}

			updateJobBase(&job.JobBase, orgrepo)
			updateUtilityConfig(&job.UtilityConfig)

			presubmit[orgrepo] = append(presubmit[orgrepo], job)
		}
	}

	// Postsubmits
	for orgrepo, post := range jobs.Postsubmits {
		orgrepo = convertOrgRepoStr(orgrepo)
		if orgrepo == "" {
			continue
		}

		for _, job := range post {
			valid := validateJob(job.Name, job.Branches)
			if !valid {
				continue
			}

			updateJobBase(&job.JobBase, orgrepo)
			updateUtilityConfig(&job.UtilityConfig)

			postsubmit[orgrepo] = append(postsubmit[orgrepo], job)
		}
	}

	writeOutFile(outPath, presubmit, postsubmit)

	return nil
}

// init entry point.
func init() {
	var _repoWhitelist []string

	var _repoBlacklist []string

	var _jobWhitelist []string

	var _jobBlacklist []string

	// --bucket
	flag.StringVar(&bucket, "bucket", "istio-private-build", "GCS bucket name to upload logs and build artifacts to.")

	// --branches
	flag.StringSliceVar(&branches, "branches", []string{}, "Branches to generate job(s) for.")

	// --cluster
	flag.StringVar(&cluster, "cluster", "private", "GCP cluster to run the job(s) in.")

	// --clean
	flag.BoolVar(&clean, "clean", false, "Clean output directory before job(s) generation.")

	// --ssh-key-secret
	flag.StringVar(&sshKeySecret, "ssh-key-secret", "ssh-key-secret", "GKE cluster secrets containing the Github ssh private key.")

	// -l, --labels
	flag.StringToStringVarP(&labels, "labels", "l", map[string]string{"preset-service-account": "true"}, "Prow labels to apply to the job(s).")

	// -i, --input
	flag.StringVarP(&input, "input", "i", ".", "Input directory containing job(s) to convert.")

	// -o, --output
	flag.StringVarP(&output, "output", "o", ".", "Output directory to write generated job(s).")

	// -w, --repo-repoWhitelist
	flag.StringSliceVarP(&_repoWhitelist, "repo-whitelist", "w", []string{}, "Repositories to whitelist in generation process.")

	// -b, --repo-repoBlacklist
	flag.StringSliceVarP(&_repoBlacklist, "repo-blacklist", "b", []string{}, "Repositories to blacklist in generation process.")

	// --job-repoWhitelist
	flag.StringSliceVar(&_jobWhitelist, "job-whitelist", []string{}, "Job(s) to whitelist in generation process.")

	// --job-repoBlacklist
	flag.StringSliceVar(&_jobBlacklist, "job-blacklist", []string{}, "Jos(s) to blacklist in generation process.")

	// -m, --mapping
	flag.StringToStringVarP(&orgMap, "mapping", "m", map[string]string{}, "Mapping between public and private Github organization(s).")

	flag.Parse()

	repoWhitelist = sets.NewString(_repoWhitelist...)
	repoBlacklist = sets.NewString(_repoBlacklist...)
	jobWhitelist = sets.NewString(_jobWhitelist...)
	jobBlacklist = sets.NewString(_jobBlacklist...)

	validateFlags()
}

// main entry point.
func main() {
	if clean {
		cleanOutPath(output)
	}

	_ = filepath.Walk(input, walkTree)
}
