# Security policy

## Supported versions

There is no release yet. When there is one, only the latest release gets security fixes. OpenAgentix Software Factory will be at version 0.x and the interfaces will still be changing, so older releases will not be patched. Fixes land on `main` and in a new patch release. See [CHANGELOG.md](CHANGELOG.md).

| Version | Supported |
|---|---|
| Latest release | none exists yet |
| Older releases | no |
| `main` | best effort |

## Reporting a vulnerability

Do not report a vulnerability in a public issue, a pull request or a Discussion.

Use GitHub private vulnerability reporting:

1. Open the repository on GitHub: https://github.com/openagentix/factory
2. Go to the **Security** tab.
3. Choose **Report a vulnerability**.

Only the maintainers can read the report. If you are not sure whether something is a vulnerability, report it privately anyway. A private report can be made public later. A public issue cannot be made private.

This project does not publish an email address for security reports.

### What to include

- What you found, and which part of the repository it is in.
- The version or commit.
- Steps to reproduce, with the smallest example you can make.
- What an attacker gains, and what they need first (for example, the right to write a ticket or a pull request comment).
- Any fix or workaround you have in mind.
- Whether you plan to publish, and when.

Do not include real keys, tokens or personal data. Use made-up values.

## What to expect

| Step | Target |
|---|---|
| We acknowledge your report | within 3 working days |
| First assessment (is it a vulnerability, how serious, who is affected) | within 14 days |
| Fix or a written plan for one | depends on severity; we will tell you the date we aim for |

The project has one maintainer today, so these are targets, not guarantees. If you hear nothing after the acknowledgement time, write a comment on the private report to nudge us.

## Disclosure process

1. We confirm the problem and work out the affected versions in the private report.
2. We prepare a fix in a private fork or a draft security advisory, and test it.
3. We agree a disclosure date with you. The default is up to 90 days after the report, and sooner when a fix is ready. Actively exploited problems are handled faster.
4. We publish a GitHub security advisory, with a CVE where it is warranted, and a patched release. The `CHANGELOG.md` entry sits under "Security". We credit you if you want to be credited.

We do not take legal action against people who report in good faith and keep to this process.

## Scope

In scope: the code and configuration in this repository, and documentation that tells people to do something unsafe.

Out of scope, report it to the upstream project: the Claude Code and Codex programs, Git, GitHub, and other tools the factory starts or talks to. If you are not sure which side a problem is on, report it to us privately and we will route it.

## Honest limits of local mode

The first rung of the product runs on one machine with no container. The factory starts the coding agent as the person who runs it, in a git worktree, and removes git credentials from the agent's environment where it can. That is best effort. The honest statement is: the same trust as running the agent yourself.

- An agent run has no credential that can merge or deploy. The git host must enforce branch protection. The owner of the repository must set it.
- Outbound network access is open in local mode. A prompt attack in a ticket could try to send data out. Use a model key with a low spend limit and do not run on a repository with secrets in its history.
- Ticket and pull request text is untrusted input and reaches the agent's prompt.
- Container isolation and enforced limits are planned for later rungs. They are not built.

The full list of threats, controls and what remains is in the threat model: [docs/decisions/0002-threat-model.md](docs/decisions/0002-threat-model.md). That record is a proposal and has not had an independent security review.

Documented limits are not vulnerabilities. A bypass of a protection the project says it provides is in scope.

## Security principles in this repository

- **Off by default, deny by default.**
- **No secrets in the repository.** Nothing prints, logs or commits a secret or token.
- **Harness logins stay where they are.** The factory never reads, copies or forwards a login token of Claude Code or Codex.
- **No skip-all-permissions flag.** The factory never starts an agent with one.
- **Untrusted text stays untrusted.** Ticket and pull request text is data, not instructions.
- **The checks that judge a run cannot be edited by the run.**

These are design rules. Nothing here has been proven by a run yet. The project is a design with one tested package. Do not rely on it for a hostile environment.
