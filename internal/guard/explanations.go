package guard

import (
	"fmt"
	"strings"

	"github.com/VBenevides/pig-plugins/internal/lancet"
)

// lancetReviewNote explains a LANCET Review verdict. A verdict without the model's uncertainty band came from a
// refusal before scoring, so its reason is shown as given.
func lancetReviewNote(verdict lancet.Result) string {
	score := ScoreText(verdict.Score)
	switch verdict.Reason {
	case "uncertainty-band":
		return "LANCET review (score " + score + "): the local model could not tell whether this command is safe or risky. Check it before you allow it."
	case "":
		return "LANCET review (score " + score + "): the local model asks you to check this command."
	}
	return "LANCET review (score " + score + "): the local model did not score this command (" + verdict.Reason + "). Check it before you allow it."
}

// behaviorExplanations say, in one sentence each, what can go wrong when a behavior runs. They are shown in the
// confirmation dialog so the user can judge the risk without decoding the command.
var behaviorExplanations = map[string]string{
	"recursive-force-delete":   "Deletes files and folders recursively without asking. Deleted data cannot be recovered from the trash.",
	"delete-root":              "Targets the root directory. It can erase the whole system.",
	"delete-sys-dir":           "Targets a system directory. It can leave the operating system broken or unbootable.",
	"fork-bomb":                "Starts processes without limit until the machine runs out of resources and freezes.",
	"remote-fetch-exec":        "Downloads a script from the network and runs it at once. You cannot review the code first.",
	"write-sensitive-file":     "Writes to a system-sensitive file such as /etc/passwd, /proc or a device. This can lock you out or break the system.",
	"write-block-device":       "Writes raw data to a disk device. It can destroy the partition table and file systems.",
	"chmod-sys-dir":            "Changes owner or permissions inside a system directory. This can break system services or open a security hole.",
	"shutdown-reboot":          "Shuts down or restarts the machine. Running work and other users' sessions are lost.",
	"disk-format":              "Formats a disk or writes raw data to it. All data on the target is destroyed.",
	"mount-block-device":       "Mounts or unmounts a disk device. Unmounting a busy disk can corrupt data.",
	"force-kill":               "Ends a process at once with SIGKILL. The process cannot save its data or clean up.",
	"pkg-global-uninstall":     "Removes a globally installed package. Other projects and tools that need it stop working.",
	"git-force-push":           "Overwrites the remote history. Other people's commits can be lost.",
	"git-push-delete":          "Deletes a branch or tag on the remote. Others lose that reference.",
	"git-push-colon-ref":       "Deletes a remote branch through the :ref syntax. Others lose that branch.",
	"git-hard-reset":           "Discards all uncommitted changes in the working tree and the index. They cannot be recovered.",
	"git-clean":                "Deletes untracked files from the working tree. They are not in Git and cannot be recovered.",
	"git-branch-delete":        "Force-deletes a branch even if it is not merged. Its commits can be lost.",
	"git-tag-delete":           "Deletes a tag. Releases that point to it can lose their marker.",
	"git-stash-clear":          "Deletes every stash entry. Saved work in the stash is lost.",
	"git-stash-drop":           "Deletes a stash entry. Its saved work is lost.",
	"git-reflog-expire":        "Removes reflog entries, the safety net for recovering lost commits.",
	"git-gc-prune":             "Permanently deletes unreachable objects. Lost commits can no longer be recovered.",
	"git-filter-branch":        "Rewrites the whole history of the repository. Commit hashes change and old clones diverge.",
	"git-filter-repo":          "Rewrites the whole history of the repository. Commit hashes change and old clones diverge.",
	"git-commit-amend":         "Replaces the last commit. If it is already pushed, a force push is needed to publish the change.",
	"git-rebase":               "Rewrites commits and changes their hashes. Conflicts or a later force push can lose work.",
	"git-remote-rm":            "Removes a remote and its tracking branches from the repository settings.",
	"git-submodule-deinit":     "Unregisters a submodule and clears its working tree. Local changes in it are lost.",
	"git-worktree-remove":      "Deletes a linked worktree and its files. Uncommitted changes there are lost.",
	"git-update-ref-delete":    "Deletes a Git reference directly. Commits that only it reaches become unreachable.",
	"git-checkout-discard":     "Overwrites every changed file in the working tree with the committed version. The changes are lost.",
	"git-restore-discard":      "Overwrites every changed file in the working tree with the committed version. The changes are lost.",
	"git-config-global":        "Changes your global Git settings. It affects every repository for this user.",
	"git-notes-remove":         "Removes Git notes attached to commits. The notes are lost.",
	"sudo":                     "Runs with administrator rights. Mistakes can change or damage the whole system.",
	"docker-destroy":           "Removes Docker containers, images, volumes or networks. Data in removed volumes is lost.",
	"kubectl-delete":           "Deletes Kubernetes resources. Running workloads and their data can be removed.",
	"mv-sys-dir":               "Moves a system directory. Programs and the system can stop working.",
	"cp-root":                  "Copies recursively into the root directory. It can overwrite system files.",
	"git-force-push-protected": "Force-pushes to a protected branch such as main. It can overwrite shared history.",
	"delete-home":              "Targets your home directory. It can erase your files and settings.",
}

// describeRisk explains each behavior and any LANCET review note, one bullet per line, for the confirmation dialog.
// An unknown behavior id keeps its label so nothing is hidden.
func describeRisk(behaviors, labels []string, lancetNote string) string {
	var lines []string
	for i, id := range behaviors {
		label := id
		if i < len(labels) {
			label = labels[i]
		}
		if explanation, ok := behaviorExplanations[id]; ok {
			lines = append(lines, fmt.Sprintf("- %s: %s", label, explanation))
			continue
		}
		lines = append(lines, "- "+label)
	}
	if lancetNote != "" {
		lines = append(lines, "- "+lancetNote)
	}
	return strings.Join(lines, "\n")
}

// describeAffected names the folder or paths the command changes, for the confirmation dialog. A recursive delete
// lists every path it names; any other command only has the working folder to show.
func describeAffected(analysis Analysis, items []string) string {
	if len(analysis.DeleteTargets) == 0 {
		return "Working folder: " + strings.Join(items, ", ") + " (the command does not name the paths it changes)"
	}
	return "Affected paths:\n- " + strings.Join(items, "\n- ")
}
