# truffles — playbook:touch + playbook:gen skeletons.
#
#   make demo-vhs TAPE=playbook-gen
#   vhs assets/playbook-gen.tape

Output assets/screenshots/playbook-gen.gif
Output assets/screenshots/playbook-gen.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 42ms
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
Type "rm -f /tmp/demo-touch.yaml /tmp/demo-gen.yaml"
Enter
Sleep 400ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles playbook:touch -o /tmp/demo-touch.yaml"
Sleep 1.8s
Enter
Sleep 3.5s

Type "sed -n '1,28p' /tmp/demo-touch.yaml"
Sleep 1.5s
Enter
Sleep 6s

Type "./truffles playbook:gen remote-demo -o /tmp/demo-gen.yaml"
Sleep 1.8s
Enter
Sleep 3.5s

Type "head -20 /tmp/demo-gen.yaml"
Sleep 1.5s
Enter
Sleep 5s

Type "echo '✓ skeletons ready — edit and run with playbook -f'"
Sleep 1.5s
Enter
Sleep 3.5s
