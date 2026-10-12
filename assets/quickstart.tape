# truffles quickstart — org search, *run* filter, vim playbook, run, remote tip.
# README hero GIF.
#
#   make demo-vhs TAPE=quickstart
#   vhs assets/quickstart.tape

Output assets/screenshots/quickstart.gif
Output assets/screenshots/quickstart.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 40ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 300ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "rm -f demo-playbook.yaml repos-ohs.txt repos-run.txt repos-demo.txt"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles"
Sleep 1.5s
Enter
Sleep 4.5s

# 1) Full org search
Type "./truffles search -no-proxy -owner openhat-security -out repos-ohs.txt"
Sleep 2s
Enter
Sleep 8s

Type "wc -l repos-ohs.txt"
Sleep 1.2s
Enter
Sleep 3.5s

# 2) Glob filter *run*
Type "./truffles search -no-proxy -owner openhat-security '*run*' -out repos-run.txt"
Sleep 2s
Enter
Sleep 7s

Type "cat repos-run.txt"
Sleep 1.5s
Enter
Sleep 3.5s

# 3) Author playbook in vim
Type "vim demo-playbook.yaml"
Sleep 1.5s
Enter
Sleep 1.8s
Type "i"
Sleep 400ms
Type "name: openhat-run"
Enter
Type "description: search openhat-security for *run*, then scan"
Enter
Type "search:"
Enter
Type "  owner: openhat-security"
Enter
Type "  queries:"
Enter
Type '    - "*run*"'
Enter
Type "  out: repos-demo.txt"
Enter
Type "  no_proxy: true"
Enter
Type "scan:"
Enter
Type "  file: repos-demo.txt"
Enter
Type "  format: pretty"
Enter
Type "  since: 1y"
Enter
Type "  exclude_paths:"
Enter
Type "    - node_modules"
Enter
Type "    - vendor"
Enter
Type '    - "*.lock"'
Enter
Type "log_dir: /tmp/truffles-demo-logs"
Escape
Sleep 1s
Type ":wq"
Enter
Sleep 1.5s

# 4) Run playbook
Type "./truffles playbook -f demo-playbook.yaml"
Sleep 2s
Enter
Sleep 22s

# 5) Point at remote fleet
Type "head -12 remote-playbook.yaml"
Sleep 1.5s
Enter
Sleep 5s

Type "./truffles job list"
Sleep 1.5s
Enter
Sleep 5s

Type "echo '✓ local quickstart done — fleet: truffles job start -c remote-playbook.yaml'"
Sleep 2s
Enter
Sleep 4s
