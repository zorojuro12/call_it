---
name: executing-plans
description: Use when you have a written implementation plan to execute in a separate session with review checkpoints
---

# Executing Plans

## Overview

Load plan, review critically, execute all tasks, report when complete.

**Announce at start:** "I'm using the executing-plans skill to implement this plan."

**Note:** Upstream recommends `subagent-driven-development` (a fresh subagent
per task) where subagents are available. That skill is deliberately not
installed in this project — inline execution is the default here, for tighter
control over what runs. This skill is the execution path for `call_it`.

## The Process

### Step 1: Load and Review Plan
1. Ensure a feature branch exists: `git checkout -b <phase-or-feature-slug> dev`
   (per `docs/dev-workflow-guide.md` §8 — branch per phase, incremental
   commits, self-merge into `dev`, no PR). The `using-git-worktrees` skill is
   available if real directory isolation is ever needed — e.g. two concurrent
   sessions on different phases — but a plain branch is the default here.
2. Read plan file
3. Review critically - identify any questions or concerns about the plan
4. If concerns: Raise them with your human partner before starting
5. If no concerns: Create todos for the plan items and proceed

### Step 2: Execute Tasks

For each task:
1. Mark as in_progress
2. Follow each step exactly (plan has bite-sized steps)
3. Run verifications as specified
4. Mark as completed

### Step 3: Complete Development

After all tasks complete and verified:
- Announce: "I'm using the finishing-a-development-branch skill to complete this work."
- **REQUIRED SUB-SKILL:** Use superpowers:finishing-a-development-branch
- Follow that skill to verify tests, present options, execute choice

## When to Stop and Ask for Help

**STOP executing immediately when:**
- Hit a blocker (missing dependency, test fails, instruction unclear)
- Plan has critical gaps preventing starting
- You don't understand an instruction
- Verification fails repeatedly

**Ask for clarification rather than guessing.**

## When to Revisit Earlier Steps

**Return to Review (Step 1) when:**
- Partner updates the plan based on your feedback
- Fundamental approach needs rethinking

**Don't force through blockers** - stop and ask.

## Remember
- Review plan critically first
- Follow plan steps exactly
- Don't skip verifications
- Reference skills when plan says to
- Stop when blocked, don't guess
- Never start implementation on `main` or `dev` without explicit user consent —
  in this project `dev` is the protected integration branch, not `main`
