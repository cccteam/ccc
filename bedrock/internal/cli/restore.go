package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-playground/errors/v5"
	"github.com/spf13/cobra"

	"github.com/cccteam/ccc/bedrock/internal/derive"
	"github.com/cccteam/ccc/bedrock/internal/prompt"
)

// restoreUse names the command.
const restoreUse = "restore"

// newRestore is restore <env> [<release>] [--reason <why>] [--before <release> | --at <moment> |
// --backup <name>] [--of <database>].
// restoreLong is the restore command's help.
const restoreLong = `restore returns an environment's database, started from GitHub: developers authenticate to GitHub and
nowhere else, and nobody sets up a cloud tool to operate an environment. It is the database step
after a release that went wrong, when the code's rollback (bedrock rollback) was not enough or the
migration itself was the fault; the two are separate runs, one after the other. There are two kinds.

A restore to a backup, in any environment, production included: --before <release> restores that
release's pre-release backup, the database as it was before that release's migrations; --at
<moment> restores a backup made as of the moment (RFC 3339), of the live database or, with --of
<database>, of an earlier generation whose history holds the moment; --backup <name> restores any
backup on the instance by its resource name, a forensic backup to undo a restore, say. The
workflow's job reads the environment's deployment records for the live release, which stays
unless another is named, and for the release's cut; bedrock backups <env> lists what there is.
The build puts the application into maintenance, keeps the live database, protected, as the
forensic copy with a backup of it taken as of that moment (thirty days), restores the chosen
backup into the database's next generation (<database>-2, then -3), runs the release's migrations
on it where the backup's schema is behind the release, deploys and takes the traffic, and
records all of it. No database an earlier run left is ever put back into service: a restore to
the wrong place is followed by another restore, and nothing is dropped. The command prints the
statement and asks for the environment's name typed before it dispatches.

The environment's own restore, below production: staging runs a release against production's
data before production does (the staging rehearsal: a release that carries migrations production
has not applied restores staging from production's newest backup in its own build), so restore
stg with no source is the manual way to bring staging's data current between releases and the
way back to production's release after a failed release there; the release may be left out, and
production's live release is what staging returns to, read by the job from production's records.
The first environment and a seeded one restore to an empty database, which has no production
state to return to, so they name their release. Production has no restore of this kind.

Every restore is a build of the environment's version trigger with the restore instruction,
started by the operations workflow's job in the GitHub Environment named after the environment,
which exchanges its GitHub token for the environment's operations identity. Everything that
changes the environment happens inside that run, as the deploy identity; the record carries the
reason, if given, and who asked. A release waits for its approval in Cloud Build as any release
does there, production's every time. The command itself changes nothing.`

func newRestore(d deps) *cobra.Command {
	var dirFlag, placementFlag, reason, before, at, backup, of string
	cmd := &cobra.Command{
		Use:   restoreUse + " <env> [<release>] [--reason <why>] [--before <release> | --at <moment> | --backup <name>] [--of <database>]",
		Short: "Return an environment's database to a backup, a moment or a release's cut, or refresh it, started from GitHub",
		Long:  restoreLong,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			env, tag := args[0], ""
			if len(args) == 2 {
				tag = args[1]
			}
			rc, err := d.operationTarget(dirFlag, placementFlag, env)
			if err != nil {
				return err
			}
			reason = strings.TrimSpace(reason)
			if strings.Contains(reason, "|") {
				return errors.New("the reason carries a | character, which the build's substitutions cannot; say it another way")
			}
			sources := 0
			for _, s := range []string{before, at, backup} {
				if s != "" {
					sources++
				}
			}
			switch {
			case sources > 1:
				return errors.New("--before, --at and --backup name the backup three ways: give one")
			case of != "" && at == "":
				return errors.New("--of names the generation a moment is read from: it goes with --at")
			case at != "":
				moment, err := time.Parse(time.RFC3339, at)
				if err != nil {
					return errors.Newf("--at %q is not an RFC 3339 moment (2026-10-05T04:30:00Z)", at)
				}
				backup = "@" + moment.UTC().Format(time.RFC3339)
			case backup != "" && !strings.HasPrefix(backup, "projects/"):
				return errors.Newf("--backup %q is not a backup's resource name (projects/<p>/instances/<i>/backups/<b>); bedrock backups %s lists them", backup, env)
			}
			out := cmd.OutOrStdout()
			if sources == 0 {
				return ownRestore(cmd, d, rc, env, tag, out)
			}
			data := "the backup " + backup
			switch {
			case before != "":
				data = "the data as it was before " + before + " (its pre-release backup)"
			case at != "" && of != "":
				data = "the data as of " + at + " (a backup of " + of + " made as of then)"
			case at != "":
				data = "the data as of " + at + " (a backup made as of then)"
			}
			stays := "its live release"
			if tag != "" {
				stays = tag
			}
			why := "no reason given"
			if reason != "" {
				why = reason
			}
			fmt.Fprintf(out, "=== RESTORE of %s: %s's database returns to %s, on %s, asked for by you: %s ===\n", env, rc.repo, data, stays, why)
			fmt.Fprintln(out, "The application goes into maintenance. The live database is kept as the forensic copy and a backup of it is taken. The backup is restored into the database's next generation; the release's migrations run on it where the backup's schema is behind the release; the release deploys and takes the traffic; the record names all of it. Writes made after the backup's moment are in the forensic copy alone.")
			typed, err := prompt.New(cmd.InOrStdin(), out).Line("Type the environment's name (" + env + ") to ask for it, anything else to stop:")
			if err != nil {
				return errors.Wrap(err, "a restore is asked for by typing the environment's name")
			}
			if typed != env {
				return errors.Newf("%q is not %s: the restore was not asked for, and nothing changed", typed, env)
			}
			login, err := dispatchOperation(cmd.Context(), d, rc, env, tag, map[string]string{actionInput: actionRestore, reasonInput: reason, backupInput: backup, beforeInput: before, databaseInput: of})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Asked, as %s, for %s's database to return to %s: the operations workflow of %s/%s runs it (%s). Its job reads the environment's records for the release that stays and the backup, prints them, runs the version trigger with the restore instruction and waits for the approval in Cloud Build, thirty minutes at most; the record names you%s.\n",
				login, env, data, rc.owner, rc.repo, workflowURL(rc), reasonSuffix(reason))

			return nil
		},
	}
	cmd.Flags().StringVar(&dirFlag, "dir", "", "the stack directory holding the placement (default: the application's stack, found from the working directory)")
	cmd.Flags().StringVar(&placementFlag, "placement", "", "placement file (default: placement.json in the stack directory)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the restore is asked for, in a sentence; the record carries it")
	cmd.Flags().StringVar(&before, "before", "", "the release whose pre-release backup to restore: the database as it was before that release's migrations")
	cmd.Flags().StringVar(&at, "at", "", "an RFC 3339 moment whose data to restore, from a backup made as of it")
	cmd.Flags().StringVar(&backup, "backup", "", "the backup to restore, by its resource name (projects/<p>/instances/<i>/backups/<b>)")
	cmd.Flags().StringVar(&of, "of", "", "with --at: the database the backup is taken of, an earlier generation whose history holds the moment (default: the live one)")

	return cmd
}

// ownRestore is the environment's own restore, below production: an empty database, or
// production's backup for the environment on production's instance.
func ownRestore(cmd *cobra.Command, d deps, rc *repositoryContext, env, tag string, out interface{ Write([]byte) (int, error) }) error {
	if env == rc.placement.Production() {
		return errors.Newf("%s is production, whose database is restored to a backup: --before <release>, --at <moment> or --backup <name> (bedrock backups %s lists them); it is never emptied by a run, and a hotfix is based on the release production runs", env, env)
	}
	kind := rc.placement.RestoreKind(env)
	if tag == "" && kind != derive.RestoreBackup {
		return errors.Newf("%s restores to an empty database (%s), which has no production state to return to: name the release to run", env, kind)
	}
	login, err := dispatchOperation(cmd.Context(), d, rc, env, tag, map[string]string{actionInput: actionRestore})
	if err != nil {
		return err
	}
	switch {
	case tag == "":
		fmt.Fprintf(out, "Asked, as %s, for %s to be restored to production's live release: the operations workflow of %s/%s runs it (%s). Its job reads the release from production's deployment records and says which; the run replaces %s's database (%s), deploys that release, and its record names you.\n",
			login, env, rc.owner, rc.repo, workflowURL(rc), env, kind)
	case kind == derive.RestoreBackup:
		fmt.Fprintf(out, "Asked, as %s, for %s to be restored to %s: the operations workflow of %s/%s runs it (%s). Its job says whether %s is production's live release, the one a restore from production's backup returns to when none is named; the run replaces %s's database (%s), deploys %s, and its record names you.\n",
			login, env, tag, rc.owner, rc.repo, workflowURL(rc), tag, env, kind, tag)
	default:
		fmt.Fprintf(out, "Asked, as %s, for %s to be restored to %s: the operations workflow of %s/%s runs it (%s). The run replaces %s's database (%s), deploys %s, and its record names you.\n",
			login, env, tag, rc.owner, rc.repo, workflowURL(rc), env, kind, tag)
	}

	return nil
}

// reasonSuffix is " and the reason" when one was given.
func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}

	return " and the reason"
}
