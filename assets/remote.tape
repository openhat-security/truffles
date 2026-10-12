# truffles — remote-playbook.yaml + live GCE fleet (status / job / clean-disk dry-run).
# Uses the real worker fleet via remote.env — read-only / dry-run only.
#
#   make demo-vhs TAPE=remote
#   vhs assets/remote.tape

Output assets/screenshots/remote.gif
Output assets/screenshots/remote.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 14
Set Width 1400
Set Height 900
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
Type "test -f remote.env || { echo 'missing remote.env'; exit 1; }"
Enter
Sleep 800ms
Type "clear"
Enter
Sleep 500ms
Show

Type "sed -n '1,45p' remote-playbook.yaml"
Sleep 1.8s
Enter
Sleep 8s

Type "grep -E '^(  - name:|    host:|    workers:|    gce_)' remote-playbook.yaml | head -40"
Sleep 2s
Enter
Sleep 6s

Type "./truffles cluster status -c remote-playbook.yaml"
Sleep 2s
Enter
Sleep 8s

Type "./truffles job list"
Sleep 1.5s
Enter
Sleep 5s

Type "./truffles job status --color always"
Sleep 1.8s
Enter
Sleep 12s

Type "./truffles cluster clean-disk -c remote-playbook.yaml --orphans-only --dry-run"
Sleep 2.2s
Enter
Sleep 10s

Type "echo '✓ remote fleet live — start with: truffles job start -c remote-playbook.yaml'"
Sleep 2s
Enter
Sleep 4.5s
