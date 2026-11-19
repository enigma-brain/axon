package devenv

// This file holds the help text of every axon devenv command. The --help
// output and the command reference in docs/reference/devenv.md are both
// produced from it; run "go generate ./..." after editing it.
//
// Each command's text describes what the SkyPilot command of the same name
// does, in SkyPilot 0.13.0, and what axon does around it.

// command is one covered command and its help.
type command struct {
	// words are the command's name after "axon devenv", and the words of the
	// sky command it runs.
	words []string
	// group is the heading the command is listed under.
	group string
	// summary is one line, without a final full stop.
	summary string
	// args is the usage after "axon devenv <words>".
	args string
	// description is paragraphs separated by blank lines. A paragraph whose
	// lines begin with "- " is a list.
	description string
	flags       []flag
	examples    []example
}

// flag is one of the flags a command's help names.
type flag struct {
	names string
	text  string
}

// example is a command line with a comment saying what it does.
type example struct {
	comment string
	line    string
}

// groups lists the group headings in the order they are shown.
var groups = []string{
	"Dev environments",
	"GPUs",
	"Managed jobs",
	"Volumes",
}

// commands is every command axon devenv covers, in the order they are shown.
var commands = []command{
	{
		words:   []string{"launch"},
		group:   "Dev environments",
		summary: "Launch a cluster, or run a task on one",
		args:    "[flags] [TASK_YAML | COMMAND...]",
		description: `Provisions a cluster and runs a task on it. When a cluster with the name
given by -c exists, it is reused.

The entrypoint is a task YAML file, or else a shell command. With no
entrypoint, the cluster is launched and nothing runs on it. The task runs in
its workdir, when one is set, and goes through the cluster's job queue.`,
		flags: []flag{
			{"-c, --cluster NAME", "The cluster's name. Without it, a new cluster gets a generated name."},
			{"--gpus TYPE[:COUNT]", "The GPUs to use, such as A100:2. Overrides the task YAML."},
			{"--cpus COUNT", "The vCPUs each node needs, such as 8, or 8+ for at least 8."},
			{"--memory GB", "The memory each node needs, such as 32, or 32+ for at least 32."},
			{"--num-nodes COUNT", "The number of nodes to run the task on."},
			{"--workdir DIR", "A local directory to sync to the cluster and run the task in."},
			{"--env NAME[=VALUE]", "An environment variable to set on the cluster. Without a value, the local one is used. Repeatable."},
			{"-i, --idle-minutes-to-autostop MINUTES", "Stop the cluster after this many idle minutes."},
			{"--down", "Tear the cluster down after its jobs finish, or after the idle time given with -i."},
			{"-d, --detach-run", "Return once the job is submitted, without streaming its logs."},
			{"--no-setup", "Skip the setup phase when relaunching a cluster."},
			{"--fast", "When the cluster is up, skip provisioning and setup."},
			{"-y, --yes", "Skip the confirmation prompt."},
		},
		examples: []example{
			{"Launch a cluster named dev from a task file", "axon devenv launch -c dev task.yaml"},
			{"Launch a cluster with one A100 and run nothing on it", "axon devenv launch -c dev --gpus A100"},
			{"Stop it after an hour without jobs or SSH sessions", "axon devenv launch -c dev -i 60 task.yaml"},
		},
	},
	{
		words:   []string{"status"},
		group:   "Dev environments",
		summary: "Show clusters",
		args:    "[flags] [CLUSTER...]",
		description: `Shows the named clusters, or every cluster the user owns, with each one's
resources, status and autostop setting.

A cluster's status is one of:

- INIT: being provisioned or set up, or in an abnormal state. Launching it
  again can recover it.
- UP: provisioned and set up.
- STOPPED: stopped, with its disks kept.

The command also updates the local SSH configuration, so that "ssh CLUSTER"
and VS Code reach each cluster.`,
		flags: []flag{
			{"-v, --verbose", "Show every field."},
			{"-r, --refresh", "Query the clusters' latest status rather than the recorded one."},
			{"-u, --all-users", "Show the clusters of every user."},
			{"--ip", "Show the IP address of the cluster's head node. Takes one cluster."},
			{"--endpoints", "Show the cluster's exposed endpoints. Takes one cluster."},
			{"--endpoint PORT", "Show the URL of one exposed port. Takes one cluster."},
			{"-o, --output table|json", "The output format."},
		},
		examples: []example{
			{"Show your clusters", "axon devenv status"},
			{"Show one cluster with every field", "axon devenv status -v dev"},
		},
	},
	{
		words:   []string{"down"},
		group:   "Dev environments",
		summary: "Tear down clusters",
		args:    "[flags] [CLUSTER...]",
		description: `Tears down the named clusters. Every resource of a cluster is deleted,
and the data on its disks is lost.

A cluster name can be a glob pattern.`,
		flags: []flag{
			{"-a, --all", "Tear down all of your clusters."},
			{"-u, --all-users", "Tear down the clusters of every user."},
			{"-y, --yes", "Skip the confirmation prompt."},
			{"-p, --purge", "Remove the cluster from SkyPilot's records even when tearing it down fails. For troubleshooting only."},
		},
		examples: []example{
			{"Tear down one cluster", "axon devenv down dev"},
			{"Tear down every cluster whose name starts with dev", `axon devenv down "dev*"`},
		},
	},
	{
		words:   []string{"autostop"},
		group:   "Dev environments",
		summary: "Stop or tear down clusters when they are idle",
		args:    "[flags] [CLUSTER...]",
		description: `Schedules the named clusters to stop, or to be torn down with --down,
after they have been idle for a set time. A stopped cluster can be launched
again; a torn-down one cannot.

The idle time restarts when a job is submitted, the cluster restarts, an
autostop setting is made, or, by default, an SSH session is active. Setting
autostop on a cluster that is already idle starts the count from that moment.`,
		flags: []flag{
			{"-i, --idle-minutes MINUTES", "The idle minutes before the cluster stops."},
			{"--down", "Tear the cluster down instead of stopping it."},
			{"--cancel", "Cancel the cluster's autostop setting."},
			{"--wait-for jobs_and_ssh|jobs|none", "What counts as activity: jobs and SSH sessions (the default), jobs only, or nothing, which stops the cluster once the time has passed."},
			{"-a, --all", "Apply to all of your clusters."},
			{"-y, --yes", "Skip the confirmation prompt."},
		},
		examples: []example{
			{"Stop dev after an hour idle", "axon devenv autostop dev -i 60"},
			{"Tear dev down after an hour idle", "axon devenv autostop dev -i 60 --down"},
			{"Cancel dev's autostop", "axon devenv autostop dev --cancel"},
		},
	},
	{
		words:   []string{"exec"},
		group:   "Dev environments",
		summary: "Run a task or command on an existing cluster",
		args:    "[flags] CLUSTER TASK_YAML | COMMAND...",
		description: `Runs a task or command on a cluster that is already up, without
provisioning it or running setup again. The workdir is synced when one is
set. The entrypoint is a task YAML file, or else a shell command.

The command goes through the cluster's job queue and runs without a
terminal, so interactive programs do not work; use "ssh CLUSTER" for them.
When the task's setup, file mounts or resources change, use launch instead.`,
		flags: []flag{
			{"--gpus TYPE[:COUNT]", "The GPUs the task needs on the cluster, such as A100:1."},
			{"--num-nodes COUNT", "The number of nodes to run the task on."},
			{"--workdir DIR", "A local directory to sync to the cluster and run the task in."},
			{"--env NAME[=VALUE]", "An environment variable to set for the task. Without a value, the local one is used. Repeatable."},
			{"-d, --detach-run", "Return once the job is submitted, without streaming its logs."},
		},
		examples: []example{
			{"Run the task file's run commands on dev", "axon devenv exec dev task.yaml"},
			{"Run a command on dev", "axon devenv exec dev python train.py"},
		},
	},
	{
		words:   []string{"logs"},
		group:   "Dev environments",
		summary: "Show the logs of a job on a cluster",
		args:    "[flags] CLUSTER [JOB_ID...]",
		description: `Streams the log of a job on the cluster, or of its latest job when no
job is named. The command exits non-zero when the job fails or its log
cannot be fetched.`,
		flags: []flag{
			{"--no-follow", "Print the log so far and exit."},
			{"--status", "Print the job's status instead of its log, and exit zero only when the job succeeded."},
			{"-s, --sync-down", "Download the logs of the named jobs to ~/sky_logs."},
			{"--provision", "Show the cluster's provisioning log."},
			{"-w, --worker N", "With --provision, show the provisioning log of worker N, counting from 1, instead of the head node's."},
		},
		examples: []example{
			{"Follow the latest job on dev", "axon devenv logs dev"},
			{"Print the log of job 3 so far", "axon devenv logs dev 3 --no-follow"},
		},
	},
	{
		words:   []string{"queue"},
		group:   "Dev environments",
		summary: "Show the job queue of clusters",
		args:    "[flags] [CLUSTER...]",
		description: `Shows the jobs on the named clusters, or on every cluster of the user's
when none is named.`,
		flags: []flag{
			{"-s, --skip-finished", "Show only pending and running jobs."},
			{"-u, --all-users", "Show the jobs of every user."},
			{"-o, --output table|json", "The output format."},
		},
		examples: []example{
			{"Show the jobs on dev", "axon devenv queue dev"},
		},
	},
	{
		words:   []string{"gpus", "list"},
		group:   "GPUs",
		summary: "Show the GPUs and other accelerators that can be requested",
		args:    "[flags] [GPU]",
		description: `Shows the accelerators that can be requested and the counts of each. A
name and count shown, such as A100 with 8, is what --gpus A100:8 and a task
YAML's accelerators field accept.

Naming a GPU shows its details.`,
		flags: []flag{
			{"-a, --all", "Show every accelerator and its details."},
			{"--infra INFRA", "Show only what one infrastructure offers, given as cloud, cloud/region or k8s/context."},
			{"-v, --verbose", "Show more detail."},
			{"-o, --output table|json", "The output format."},
		},
		examples: []example{
			{"Show the accelerators", "axon devenv gpus list"},
			{"Show the details of the A100", "axon devenv gpus list A100"},
		},
	},
	{
		words:   []string{"jobs", "launch"},
		group:   "Managed jobs",
		summary: "Launch a managed job",
		args:    "[flags] TASK_YAML | COMMAND...",
		description: `Submits a managed job from a task YAML file or a shell command. SkyPilot
provisions the job's own cluster, recovers the job when its cluster fails,
and tears the cluster down when the job ends.`,
		flags: []flag{
			{"-n, --name NAME", "The job's name."},
			{"--gpus TYPE[:COUNT]", "The GPUs to use, such as A100:2. Overrides the task YAML."},
			{"--num-nodes COUNT", "The number of nodes to run the job on."},
			{"--workdir DIR", "A local directory to sync and run the job in."},
			{"--env NAME[=VALUE]", "An environment variable to set for the job. Without a value, the local one is used. Repeatable."},
			{"-d, --detach-run", "Return once the job is submitted, without streaming its logs."},
			{"-y, --yes", "Skip the confirmation prompt."},
		},
		examples: []example{
			{"Launch a job from a task file", "axon devenv jobs launch -n train task.yaml"},
		},
	},
	{
		words:   []string{"jobs", "queue"},
		group:   "Managed jobs",
		summary: "Show managed jobs",
		args:    "[flags]",
		description: `Shows the user's managed jobs and the status of each:

- PENDING: waiting to be accepted.
- STARTING: provisioning the job's cluster.
- RUNNING: running.
- RECOVERING: recovering from a failure of the job's cluster.
- SUCCEEDED: finished successfully.
- CANCELLING, CANCELLED: being cancelled, or cancelled, by the user.
- FAILED: failed in the job's own code.
- FAILED_SETUP: failed in the job's setup commands.
- FAILED_PRECHECKS: failed SkyPilot's checks, such as a resource that cannot
  be satisfied.
- FAILED_NO_RESOURCE: no resources were found after the maximum retries.
- FAILED_CONTROLLER: failed in the jobs controller.

When a job fails, "axon devenv jobs logs --controller JOB_ID" shows why.`,
		flags: []flag{
			{"-l, --limit COUNT", "Show this many jobs."},
			{"-a, --all", "Show every job."},
			{"-u, --all-users", "Show the jobs of every user."},
			{"--skip-finished", "Show only pending and running jobs."},
			{"-s, --status STATUSES", "Show only jobs in these statuses, comma-separated, such as FAILED,FAILED_SETUP."},
			{"--since AGE", "Show only jobs submitted within this time, such as 48h or 7d."},
			{"-r, --refresh", "Query the latest statuses."},
			{"-o, --output table|json", "The output format."},
		},
		examples: []example{
			{"Show your managed jobs", "axon devenv jobs queue"},
			{"Show the failed jobs of the last week", "axon devenv jobs queue -s FAILED --since 7d"},
		},
	},
	{
		words:   []string{"jobs", "logs"},
		group:   "Managed jobs",
		summary: "Show the logs of a managed job",
		args:    "[flags] [JOB_ID] [TASK]",
		description: `Streams the log of a managed job, named by its ID or with -n. A job with
several tasks shows all of them unless one task is named, by its ID or its
name.`,
		flags: []flag{
			{"-n, --name NAME", "The job's name."},
			{"--no-follow", "Print the log so far and exit."},
			{"--controller", "Show the jobs controller's log for the job, which records its launch and recoveries."},
			{"-s, --sync-down", "Download the logs to the laptop instead of printing them."},
			{"-r, --refresh", "Query the latest logs."},
		},
		examples: []example{
			{"Follow job 1", "axon devenv jobs logs 1"},
			{"Show why job 1 failed", "axon devenv jobs logs --controller 1"},
		},
	},
	{
		words:       []string{"jobs", "cancel"},
		group:       "Managed jobs",
		summary:     "Cancel managed jobs",
		args:        "[flags] [JOB_ID...]",
		description: `Cancels the managed jobs with the given IDs, or the job given with -n.`,
		flags: []flag{
			{"-n, --name NAME", "The name of the job to cancel."},
			{"-a, --all", "Cancel all of your managed jobs."},
			{"-y, --yes", "Skip the confirmation prompt."},
		},
		examples: []example{
			{"Cancel jobs 1 and 2", "axon devenv jobs cancel 1 2"},
			{"Cancel the job named train", "axon devenv jobs cancel -n train"},
		},
	},
	{
		words:       []string{"volumes", "ls"},
		group:       "Volumes",
		summary:     "List volumes",
		args:        "[flags]",
		description: `Lists the volumes SkyPilot manages.`,
		flags: []flag{
			{"-v, --verbose", "Show every field."},
			{"-r, --refresh", "Query the volumes' latest state rather than the recorded one."},
		},
		examples: []example{
			{"List volumes", "axon devenv volumes ls"},
		},
	},
}
