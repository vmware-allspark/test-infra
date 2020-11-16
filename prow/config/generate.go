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

package config

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	"github.com/kr/pretty"
	"gopkg.in/robfig/cron.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	prowjob "k8s.io/test-infra/prow/apis/prowjobs/v1"
	"k8s.io/test-infra/prow/config"
)

func exit(err error, context string) {
	if context == "" {
		_, _ = fmt.Fprintf(os.Stderr, "%v\n", err)
	} else {
		_, _ = fmt.Fprintf(os.Stderr, "%v: %v\n", context, err)
	}
	os.Exit(1)
}

const (
	TestGridDashboard   = "testgrid-dashboards"
	TestGridAlertEmail  = "testgrid-alert-email"
	TestGridNumFailures = "testgrid-num-failures-to-alert"

	AutogenHeader = "# THIS FILE IS AUTOGENERATED. See prow/config/README.md\n"

	DefaultResource      = "default"
	DefaultMemoryRequest = "3Gi"
	DefaultCPURequest    = "1000m"
	DefaultMemoryLimit   = "24Gi"
	DefaultCPULimit      = "3000m"

	ModifierHidden   = "hidden"
	ModifierOptional = "optional"
	ModifierSkipped  = "skipped"

	TypePostsubmit = "postsubmit"
	TypePresubmit  = "presubmit"
	TypePeriodic   = "periodic"

	RequirementRoot    = "root"
	RequirementKind    = "kind"
	RequirementDocker  = "docker"
	RequirementCache   = "cache"
	RequirementGitHub  = "github"
	RequirementRelease = "release"
	RequirementGCP     = "gcp"
	RequirementDeploy  = "deploy"
)

var AllRequirements = []string{
	RequirementKind,
	RequirementDocker,
	RequirementGitHub,
	RequirementRelease,
	RequirementRoot,
	RequirementGCP,
	RequirementDeploy,
	RequirementCache,
}

type JobConfig struct {
	Jobs                    []Job                              `json:"jobs,omitempty"`
	Repo                    string                             `json:"repo,omitempty"`
	Org                     string                             `json:"org,omitempty"`
	Branches                []string                           `json:"branches,omitempty"`
	Env                     []v1.EnvVar                        `json:"env,omitempty"`
	Resources               map[string]v1.ResourceRequirements `json:"resources,omitempty"`
	Image                   string                             `json:"image,omitempty"`
	ImagePullPolicy         string                             `json:"image_pull_policy,omitempty"`
	SupportReleaseBranching bool                               `json:"support_release_branching,omitempty"`
	NodeSelector            map[string]string                  `json:"node_selector,omitempty"`
	Requirements            []string                           `json:"requirements,omitempty"`
}

type Job struct {
	Name                    string            `json:"name,omitempty"`
	PostsubmitName          string            `json:"postsubmit,omitempty"`
	Command                 []string          `json:"command,omitempty"`
	Env                     []v1.EnvVar       `json:"env,omitempty"`
	Resources               string            `json:"resources,omitempty"`
	Modifiers               []string          `json:"modifiers,omitempty"`
	Requirements            []string          `json:"requirements,omitempty"`
	Type                    string            `json:"type,omitempty"`
	Timeout                 *prowjob.Duration `json:"timeout,omitempty"`
	Repos                   []string          `json:"repos,omitempty"`
	Image                   string            `json:"image,omitempty"`
	ImagePullPolicy         string            `json:"image_pull_policy,omitempty"`
	Interval                string            `json:"interval,omitempty"`
	Cron                    string            `json:"cron,omitempty"`
	Regex                   string            `json:"regex,omitempty"`
	Cluster                 string            `json:"cluster,omitempty"`
	MaxConcurrency          int               `json:"max_concurrency,omitempty"`
	DisableReleaseBranching bool              `json:"disable_release_branching,omitempty"`
	NodeSelector            map[string]string `json:"node_selector,omitempty"`
}

// Reads the job yaml
func ReadJobConfig(file string) JobConfig {
	yamlFile, err := ioutil.ReadFile(file)
	if err != nil {
		exit(err, "failed to read "+file)
	}
	jobs := JobConfig{}
	if err := yaml.Unmarshal(yamlFile, &jobs); err != nil {
		exit(err, "failed to unmarshal "+file)
	}

	if len(jobs.Branches) == 0 {
		jobs.Branches = []string{"master"}
	}

	if _, ok := jobs.Resources[DefaultResource]; !ok {
		if jobs.Resources == nil {
			jobs.Resources = make(map[string]v1.ResourceRequirements)
		}

		jobs.Resources[DefaultResource] = v1.ResourceRequirements{
			Limits: v1.ResourceList{
				"memory": resource.MustParse(DefaultMemoryLimit),
				"cpu":    resource.MustParse(DefaultCPULimit),
			},
			Requests: v1.ResourceList{
				"memory": resource.MustParse(DefaultMemoryRequest),
				"cpu":    resource.MustParse(DefaultCPURequest),
			},
		}
	}

	return jobs
}

// Writes the job yaml
func WriteJobConfig(jobConfig JobConfig, file string) error {
	bytes, err := yaml.Marshal(jobConfig)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(file, bytes, 0644)
}

func ValidateJobConfig(jobConfig JobConfig) {
	var err error
	if _, f := jobConfig.Resources[DefaultResource]; !f {
		err = multierror.Append(err, fmt.Errorf("'%v' resource must be provided", DefaultResource))
	}
	if jobConfig.Image == "" {
		err = multierror.Append(err, fmt.Errorf("'image' must be set"))
	}
	for _, r := range jobConfig.Requirements {
		if e := validate(
			r,
			AllRequirements,
			"requirements"); e != nil {
			err = multierror.Append(err, e)
		}
	}
	for _, job := range jobConfig.Jobs {
		if job.Resources != "" {
			if _, f := jobConfig.Resources[job.Resources]; !f {
				err = multierror.Append(err, fmt.Errorf("job '%v' has nonexistant resource '%v'", job.Name, job.Resources))
			}
		}
		for _, mod := range job.Modifiers {
			if e := validate(mod, []string{ModifierHidden, ModifierOptional, ModifierSkipped}, "status"); e != nil {
				err = multierror.Append(err, e)
			}
		}
		for _, req := range job.Requirements {
			if e := validate(
				req,
				AllRequirements,
				"requirements"); e != nil {
				err = multierror.Append(err, e)
			}
		}
		if job.Type == TypePeriodic {
			if job.Cron != "" && job.Interval != "" {
				err = multierror.Append(err, fmt.Errorf("cron and interval cannot be both set in periodic %s", job.Name))
			} else if job.Cron == "" && job.Interval == "" {
				err = multierror.Append(err, fmt.Errorf("cron and interval cannot be both empty in periodic %s", job.Name))
			} else if job.Cron != "" {
				if _, e := cron.Parse(job.Cron); e != nil {
					err = multierror.Append(err, fmt.Errorf("invalid cron string %s in periodic %s: %v", job.Cron, job.Name, e))
				}
			} else if job.Interval != "" {
				if _, e := time.ParseDuration(job.Interval); e != nil {
					err = multierror.Append(err, fmt.Errorf("cannot parse duration %s in periodic %s: %v", job.Interval, job.Name, e))
				}
			}
		}
		if e := validate(job.Type, []string{TypePostsubmit, TypePresubmit, TypePeriodic, ""}, "type"); e != nil {
			err = multierror.Append(err, e)
		}
		for _, repo := range job.Repos {
			if len(strings.Split(repo, "/")) != 2 {
				err = multierror.Append(err, fmt.Errorf("repo %v not valid, should take form org/repo", repo))
			}
		}
	}
	if err != nil {
		exit(err, "validation failed")
	}
}

func ConvertJobConfig(jobConfig JobConfig, branch string) config.JobConfig {
	var presubmits []config.Presubmit
	var postsubmits []config.Postsubmit
	var periodics []config.Periodic

	output := config.JobConfig{
		PresubmitsStatic:  map[string][]config.Presubmit{},
		PostsubmitsStatic: map[string][]config.Postsubmit{},
		Periodics:         []config.Periodic{},
	}
	for _, job := range jobConfig.Jobs {
		brancher := config.Brancher{
			Branches: []string{fmt.Sprintf("^%s$", branch)},
		}

		testgridJobPrefix := "istio"
		if branch != "master" {
			testgridJobPrefix += "_" + branch
		}
		testgridJobPrefix += "_" + jobConfig.Repo

		requirements := append(job.Requirements, jobConfig.Requirements...)

		if job.Type == TypePresubmit || job.Type == "" {
			name := fmt.Sprintf("%s_%s", job.Name, jobConfig.Repo)
			if branch != "master" {
				name += "_" + branch
			}

			presubmit := config.Presubmit{
				JobBase:   createJobBase(jobConfig, job, name, jobConfig.Repo, branch, jobConfig.Resources),
				AlwaysRun: true,
				Brancher:  brancher,
			}
			if job.Regex != "" {
				presubmit.RegexpChangeMatcher = config.RegexpChangeMatcher{
					RunIfChanged: job.Regex,
				}
				presubmit.AlwaysRun = false
			}
			presubmit.JobBase.Annotations[TestGridDashboard] = testgridJobPrefix
			applyModifiersPresubmit(&presubmit, job.Modifiers)
			applyRequirements(&presubmit.JobBase, requirements)
			presubmits = append(presubmits, presubmit)
		}

		if job.Type == TypePostsubmit || job.Type == "" {
			postName := job.PostsubmitName
			if postName == "" {
				postName = job.Name
			}

			name := fmt.Sprintf("%s_%s", postName, jobConfig.Repo)
			if branch != "master" {
				name += "_" + branch
			}
			name += "_postsubmit"

			postsubmit := config.Postsubmit{
				JobBase:  createJobBase(jobConfig, job, name, jobConfig.Repo, branch, jobConfig.Resources),
				Brancher: brancher,
			}
			if job.Regex != "" {
				postsubmit.RegexpChangeMatcher = config.RegexpChangeMatcher{
					RunIfChanged: job.Regex,
				}
			}
			postsubmit.JobBase.Annotations[TestGridDashboard] = testgridJobPrefix + "_postsubmit"
			postsubmit.JobBase.Annotations[TestGridAlertEmail] = "istio-oncall@googlegroups.com"
			postsubmit.JobBase.Annotations[TestGridNumFailures] = "1"
			applyModifiersPostsubmit(&postsubmit, job.Modifiers)
			applyRequirements(&postsubmit.JobBase, requirements)
			postsubmits = append(postsubmits, postsubmit)
		}

		if job.Type == TypePeriodic {
			name := fmt.Sprintf("%s_%s", job.Name, jobConfig.Repo)
			if branch != "master" {
				name += "_" + branch
			}
			name += "_periodic"

			// If no repos are provided, add itself to the repo list.
			if len(job.Repos) == 0 {
				job.Repos = []string{jobConfig.Org + "/" + jobConfig.Repo}
			}
			periodic := config.Periodic{
				JobBase:  createJobBase(jobConfig, job, name, jobConfig.Repo, branch, jobConfig.Resources),
				Interval: job.Interval,
				Cron:     job.Cron,
			}
			periodic.JobBase.Annotations[TestGridDashboard] = testgridJobPrefix + "_periodic"
			periodic.JobBase.Annotations[TestGridAlertEmail] = "istio-oncall@googlegroups.com"
			periodic.JobBase.Annotations[TestGridNumFailures] = "1"
			applyRequirements(&periodic.JobBase, requirements)
			periodics = append(periodics, periodic)
		}

		if len(presubmits) > 0 {
			output.PresubmitsStatic[fmt.Sprintf("%s/%s", jobConfig.Org, jobConfig.Repo)] = presubmits
		}
		if len(postsubmits) > 0 {
			output.PostsubmitsStatic[fmt.Sprintf("%s/%s", jobConfig.Org, jobConfig.Repo)] = postsubmits
		}
		if len(periodics) > 0 {
			output.Periodics = periodics
		}
	}
	return output
}

func CheckConfig(jobs config.JobConfig, currentConfigFile string) error {
	current, err := ioutil.ReadFile(currentConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read current config for %s: %v", currentConfigFile, err)
	}

	newConfig, err := yaml.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %v", err)
	}
	output := []byte(AutogenHeader)
	output = append(output, newConfig...)

	if !bytes.Equal(current, output) {
		return fmt.Errorf("generated config is different than file %v", currentConfigFile)
	}
	return nil
}

func WriteConfig(jobs config.JobConfig, fname string) {
	bytes, err := yaml.Marshal(jobs)
	if err != nil {
		exit(err, "failed to marshal result")
	}
	dir := filepath.Dir(fname)
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		exit(err, "failed to create directory: "+dir)
	}
	output := []byte(AutogenHeader)
	output = append(output, bytes...)
	err = ioutil.WriteFile(fname, output, 0644)
	if err != nil {
		exit(err, "failed to write result")
	}
}

func PrintConfig(c interface{}) {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		exit(err, "failed to write result")
	}
	fmt.Println(string(bytes))
}

func validate(input string, options []string, description string) error {
	valid := false
	for _, opt := range options {
		if input == opt {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("'%v' is not a valid %v. Must be one of %v", input, description, strings.Join(options, ", "))
	}
	return nil
}

func DiffConfig(result config.JobConfig, existing config.JobConfig) {
	fmt.Println("Presubmit diff:")
	diffConfigPresubmit(result, existing)
	fmt.Println("\n\nPostsubmit diff:")
	diffConfigPostsubmit(result, existing)
}

// FilterReleaseBranchingJobs filters then returns jobs with release branching enabled.
func FilterReleaseBranchingJobs(jobs []Job) []Job {
	jobsF := []Job{}
	for _, j := range jobs {
		if j.DisableReleaseBranching {
			continue
		}
		jobsF = append(jobsF, j)
	}
	return jobsF
}

func getPresubmit(c config.JobConfig, jobName string) *config.Presubmit {
	presubmits := c.PresubmitsStatic
	for _, jobs := range presubmits {
		for _, job := range jobs {
			if job.Name == jobName {
				return &job
			}
		}
	}
	return nil
}

func diffConfigPresubmit(result config.JobConfig, pj config.JobConfig) {
	known := make(map[string]struct{})
	for _, jobs := range result.PresubmitsStatic {
		for _, job := range jobs {
			known[job.Name] = struct{}{}
			current := getPresubmit(pj, job.Name)
			if current == nil {
				fmt.Println("\nCreated unknown presubmit job", job.Name)
				continue
			}
			diff := pretty.Diff(current, &job)
			if len(diff) > 0 {
				fmt.Println("\nDiff for", job.Name)
			}
			for _, d := range diff {
				fmt.Println(d)
			}
		}
	}
	for _, jobs := range pj.PresubmitsStatic {
		for _, job := range jobs {
			if _, f := known[job.Name]; !f {
				fmt.Println("Missing", job.Name)
			}
		}
	}
}

func diffConfigPostsubmit(result config.JobConfig, pj config.JobConfig) {
	known := make(map[string]struct{})
	allCurrentPostsubmits := []config.Postsubmit{}
	for _, jobs := range pj.PostsubmitsStatic {
		allCurrentPostsubmits = append(allCurrentPostsubmits, jobs...)
	}
	for _, jobs := range result.PostsubmitsStatic {
		for _, job := range jobs {
			known[job.Name] = struct{}{}
			var current *config.Postsubmit
			for _, ps := range allCurrentPostsubmits {
				if ps.Name == job.Name {
					current = &ps
					break
				}
			}
			if current == nil {
				fmt.Println("\nCreated unknown job:", job.Name)
				continue

			}
			diff := pretty.Diff(current, &job)
			if len(diff) > 0 {
				fmt.Println("\nDiff for", job.Name)
			}
			for _, d := range diff {
				fmt.Println(d)
			}
		}
	}

	for _, job := range allCurrentPostsubmits {
		if _, f := known[job.Name]; !f {
			fmt.Println("Missing", job.Name)
		}
	}
}

func createContainer(jobConfig JobConfig, job Job, resources map[string]v1.ResourceRequirements) []v1.Container {
	img := job.Image
	if img == "" {
		img = jobConfig.Image
	}

	imgPullPolicy := job.ImagePullPolicy
	if imgPullPolicy == "" {
		imgPullPolicy = jobConfig.ImagePullPolicy
	}

	envs := job.Env
	if len(envs) == 0 {
		envs = jobConfig.Env
	}

	c := v1.Container{
		Image:           img,
		SecurityContext: &v1.SecurityContext{Privileged: newTrue()},
		Command:         job.Command,
		Env:             envs,
		VolumeMounts: []v1.VolumeMount{{
			MountPath: "/home/prow/go/pkg",
			Name:      "build-cache",
			SubPath:   "gomod",
		}},
	}
	if imgPullPolicy != "" {
		c.ImagePullPolicy = v1.PullPolicy(imgPullPolicy)
	}
	resource := DefaultResource
	if job.Resources != "" {
		resource = job.Resources
	}
	c.Resources = resources[resource]

	return []v1.Container{c}
}

func createJobBase(jobConfig JobConfig, job Job, name string, repo string, branch string, resources map[string]v1.ResourceRequirements) config.JobBase {
	yes := true
	hostPathType := v1.HostPathDirectoryOrCreate
	jb := config.JobBase{
		Name:           name,
		MaxConcurrency: job.MaxConcurrency,
		Spec: &v1.PodSpec{
			NodeSelector: map[string]string{"testing": "test-pool"},
			Containers:   createContainer(jobConfig, job, resources),
			Volumes: []v1.Volume{{
				Name: "build-cache",
				VolumeSource: v1.VolumeSource{
					HostPath: &v1.HostPathVolumeSource{
						Path: "/tmp/prow/cache",
						Type: &hostPathType,
					},
				},
			}},
		},
		UtilityConfig: config.UtilityConfig{
			Decorate:  &yes,
			PathAlias: fmt.Sprintf("istio.io/%s", repo),
			ExtraRefs: createExtraRefs(job.Repos, branch),
		},
		Labels:      make(map[string]string),
		Annotations: make(map[string]string),
	}
	if jobConfig.NodeSelector != nil {
		jb.Spec.NodeSelector = jobConfig.NodeSelector
	}
	if job.NodeSelector != nil {
		jb.Spec.NodeSelector = job.NodeSelector
	}
	if job.Timeout != nil {
		jb.DecorationConfig = &prowjob.DecorationConfig{
			Timeout: job.Timeout,
		}
	}
	if job.Cluster != "" && job.Cluster != "default" {
		jb.Cluster = job.Cluster
	}
	return jb
}

func createExtraRefs(extraRepos []string, defaultBranch string) []prowjob.Refs {
	refs := []prowjob.Refs{}
	for _, extraRepo := range extraRepos {
		branch := defaultBranch
		repobranch := strings.Split(extraRepo, "@")
		if len(repobranch) > 1 {
			branch = repobranch[1]
		}
		orgrepo := strings.Split(repobranch[0], "/")
		org, repo := orgrepo[0], orgrepo[1]
		ref := prowjob.Refs{
			Org:     org,
			Repo:    repo,
			BaseRef: branch,
		}
		// istio uses vanity imports
		if org == "istio" {
			ref.PathAlias = fmt.Sprintf("istio.io/%s", repo)
		}
		refs = append(refs, ref)
	}
	return refs
}

func applyRequirements(job *config.JobBase, requirements []string) {
	for _, req := range requirements {
		switch req {
		case RequirementGCP:
			// The preset service account will set up the required resources
			job.Labels["preset-service-account"] = "true"
		case RequirementDeploy:
			// The preset service account will set up the required resources
			job.Labels["preset-prow-deployer-service-account"] = "true"
		case RequirementRelease:
			// Grant access to release resources, such as docker and github
			job.Labels["preset-release-pipeline"] = "true"
		case RequirementRoot:
			job.Spec.Containers[0].SecurityContext.Privileged = newTrue()
		case RequirementKind:
			// Kind requires special volumes set up for docker
			dir := v1.HostPathDirectory
			job.Spec.Volumes = append(job.Spec.Volumes,
				v1.Volume{
					Name: "modules",
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/lib/modules",
							Type: &dir,
						},
					},
				},
				v1.Volume{
					Name: "cgroup",
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/sys/fs/cgroup",
							Type: &dir,
						},
					},
				},
				v1.Volume{
					Name: "docker-root",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
				},
			)
			job.Spec.Containers[0].VolumeMounts = append(job.Spec.Containers[0].VolumeMounts,
				v1.VolumeMount{
					MountPath: "/lib/modules",
					Name:      "modules",
					ReadOnly:  true,
				},
				v1.VolumeMount{
					MountPath: "/sys/fs/cgroup",
					Name:      "cgroup",
					ReadOnly:  true,
				},
				v1.VolumeMount{
					MountPath: "/var/lib/docker",
					Name:      "docker-root",
				},
			)
		case RequirementDocker:
			// TODO in the future we can only require root if we need docker, and add entrypoint
			// Mounting a docker volume improves performance
			job.Spec.Volumes = append(job.Spec.Volumes,
				v1.Volume{
					Name: "docker-root",
					VolumeSource: v1.VolumeSource{
						EmptyDir: &v1.EmptyDirVolumeSource{},
					},
				},
			)
			job.Spec.Containers[0].VolumeMounts = append(job.Spec.Containers[0].VolumeMounts,
				v1.VolumeMount{
					MountPath: "/var/lib/docker",
					Name:      "docker-root",
				},
			)
		case RequirementCache:
			job.Spec.Containers[0].VolumeMounts = append(job.Spec.Containers[0].VolumeMounts, v1.VolumeMount{
				MountPath: "/gocache",
				Name:      "build-cache",
				SubPath:   "gocache",
			})
			// This is now default. Requirement is kept in case of future additional opt-in caching
		case RequirementGitHub:
			job.Spec.Volumes = append(job.Spec.Volumes,
				v1.Volume{
					Name: "github",
					VolumeSource: v1.VolumeSource{
						Secret: &v1.SecretVolumeSource{
							SecretName: "oauth-token",
						},
					},
				},
			)
			job.Spec.Containers[0].VolumeMounts = append(job.Spec.Containers[0].VolumeMounts,
				v1.VolumeMount{
					Name:      "github",
					MountPath: "/etc/github-token",
					ReadOnly:  true,
				},
			)
		}
	}
}

func applyModifiersPresubmit(presubmit *config.Presubmit, jobModifiers []string) {
	for _, modifier := range jobModifiers {
		if modifier == ModifierOptional {
			presubmit.Optional = true
		} else if modifier == ModifierHidden {
			presubmit.SkipReport = true
		} else if modifier == ModifierSkipped {
			presubmit.AlwaysRun = false
		}
	}
}

func applyModifiersPostsubmit(postsubmit *config.Postsubmit, jobModifiers []string) {
	for _, modifier := range jobModifiers {
		if modifier == ModifierOptional {
			// Does not exist on postsubmit
		} else if modifier == ModifierHidden {
			postsubmit.SkipReport = true
		}
		// Cannot skip a postsubmit; instead just make `type: presubmit`
	}
}

// Reads the generate job config for comparison
func ReadProwJobConfig(file string) config.JobConfig {
	yamlFile, err := ioutil.ReadFile(file)
	if err != nil {
		exit(err, "failed to read "+file)
	}
	jobs := config.JobConfig{}
	if err := yaml.Unmarshal(yamlFile, &jobs); err != nil {
		exit(err, "failed to unmarshal "+file)
	}
	return jobs
}

// kubernetes API requires a pointer to a bool for some reason
func newTrue() *bool {
	b := true
	return &b
}
