# truffles quickstart — org search, *run* filter, vim playbook, run.
# This is the README hero GIF.
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
Set TypingSpeed 34ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 200ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "rm -f demo-playbook.yaml repos-ohs.txt repos-run.txt repos-demo.txt"
Enter
Sleep 300ms
Type "clear"
Enter
Sleep 400ms
Show

Type "./truffles"
Enter
Sleep 2s

# 1) Full org search
Type "./truffles search -no-proxy -owner openhat-security -out repos-ohs.txt"
Enter
Sleep 6s

Type "wc -l repos-ohs.txt"
Enter
Sleep 1.5s

# 2) Glob filter *run*
Type "./truffles search -no-proxy -owner openhat-security '*run*' -out repos-run.txt"
Enter
Sleep 5s

Type "cat repos-run.txt"
Enter
Sleep 1.8s

# 3) Author playbook in vim (same shape as playbook.tape)
Type "vim demo-playbook.yaml"
Enter
Sleep 1.2s
Type "i"
Sleep 300ms
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
Sleep 700ms
Type ":wq"
Enter
Sleep 1s

# 4) Run playbook
Type "./truffles playbook -f demo-playbook.yaml"
Enter
Sleep 18s

Type "echo '✓ truffles quickstart complete'"
Enter
Sleep 2.5s
