# Goal

Work toward the goals of the repository described in its issues and README.
Work incrementally and visibly so others can follow along and guide the work.

# Tracking work

* Use issues for all work: epics broken down into sub-issues.
* Keep each epic's body as a live checklist of its sub-issues (done / in progress / next).
* When you or a worker find a bug, limitation, spec gap, or deferred scope item that won't be fixed in the current PR, open a focused sub-issue for it (one behavior, with a repro or screenshot) rather than only mentioning it in prose. Review findings not fixed before merge become issues too.
* Avoid duplicating information where possible.  But cross-link to ensure it's easy to connect things.  Remove information and link to new source of truth if needed.

# Implementing

* Implement changes via PRs into main. Use Actions for CI and CD.
* Before starting a sub-issue, comment on it with the planned approach (1-5 bullets).
* Push branches and PRs early, as Draft if unsure; mark Ready when possible.
* PR descriptions include a current visual where relevant, and a "Known gaps / follow-ups" list linking the issues above.
* Run independent work in parallel when it helps, and reconcile design, implementation, and merge conflicts between it via PRs.
* Do regular "clean up" passes to ensure debt isn't accruing.

# Reporting progress

* Include visuals (screenshots, renders, graphs) in issues, PRs, and docs wherever possible.
* When a PR merges, comment on its epic: what landed, a current visual, known gaps, what's next, and any decisions you made without asking. Keep it short.
* The README is the quick look at current state. Its first screen shows a current visual, what works, what's next, and links to any live previews.

# Asking for input

* When input is needed on whether, what, or how to do something, tag @lukehoban or assign the issue/PR to them.
* For design choices with meaningful tradeoffs (scope cuts, spec deviations, new dependencies, architecture), post the options and your recommendation on the issue and tag @lukehoban. Proceed with the recommendation unless the choice is hard to reverse; then wait for a reply.
* You can ask @lukehoban to add Actions variables/secrets for access to additional systems.
