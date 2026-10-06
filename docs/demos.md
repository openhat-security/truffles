# Demos

Recorded with [VHS](https://github.com/charmbracelet/vhs) against the `openhat-security` org.

```bash
brew install vhs
make demo-vhs TAPE=quickstart   # README hero
make demo-vhs-all               # every tape under assets/
```

## Gallery

<p>
<img src="../assets/screenshots/search.gif" alt="full org search" width="49%"/>
<img src="../assets/screenshots/filter.gif" alt="glob filter *run*" width="49%"/>
</p>

<p>
<img src="../assets/screenshots/playbook.gif" alt="vim playbook + run" width="49%"/>
<img src="../assets/screenshots/quickstart.gif" alt="full quickstart reel" width="49%"/>
</p>

| Tape | Story |
| --- | --- |
| `help` | `truffles -help` still (README screenshot) |
| `search` | Enumerate every public repo under `openhat-security` |
| `filter` | Glob filter `*run*` on that org |
| `playbook` | Author YAML in vim, then `truffles playbook -f` |
| `quickstart` | All of the above in one reel (README) |
