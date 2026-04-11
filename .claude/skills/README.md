# Claude skills (TalkBack)

Skills are procedural contracts in **`SKILL.md`** files under this directory. Agents should **read and follow** the relevant skill when the user’s task matches.

| Skill | When to use |
|-------|-------------|
| **`epic-run/`** | User says **`run epic SCRUM-XX`**, **`continue epic SCRUM-XX`**, or **`continue epic run for SCRUM-XX`**. Executes all epic children with **FULL_AUTO + Final Gate**; **`continue`** must **drain all remaining Jira work**, not a single ticket. If you **squash-merge yourself** after a WARN, **`continue epic`** reconciles **Jira Done**, **`main`**, and **branch cleanup**—see `epic-run/SKILL.md` (**User override**). |
| **`feature-plan/`** | User asks for a **plan** before implementation (no code yet). |
| **`repo-map/`** | Orientation and repository layout. |
| **`smoke-tests/`** | Authoring or extending **Go smoke/integration** tests. |
| **`e2e-tests/`** | Authoring or extending **Playwright** tests in `web/tests/e2e/`. |

**Epic vs `CLAUDE.md` §8:** Standalone **`implement SCRUM-XX FULL_AUTO`** follows §8 only. **Epic runs** add **Final Gate `PASS`** before merge (stricter). See **`CLAUDE.md` §10** and **`epic-run/SKILL.md`**.

**No separate `skills.md` at repo root** — this file is the index.
