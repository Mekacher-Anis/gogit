# gogit

A very basic git equivalent in golang in 700 lines of code.

I've been working with git since I started writing code, but lately I realised that I don't really understand how it works, so I decided to implement git from scratch and reinvent the wheel to get a better understanding of the git internals.

**This is for educational purposes only.**


## Supported commands

- `go run cmd/gogit.go -m message commit`
- `go run cmd/gogit.go branch new_branch_name`
- `go run cmd/gogit.go checkout branch_name`
- `go run cmd/gogit.go revert commit_hash`
- `go run cmd/gogit.go log`
- `go run cmd/gogit.go status`
- `go run cmd/gogit.go --objPath object_file_path read-obj-file`


## Todo

- [ ] Add `git add` command, currently user can't choose files to commit
- [ ] Add `git merge` command, currently user can't merge branches
- [ ] Add current changes to files in `status` command