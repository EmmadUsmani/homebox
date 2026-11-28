# Image build recipes

Every project-maintained VM image has a recipe in `images/NAME/`:

- `build.sh` builds the image end to end on a compatible Homebox host using `box`.
- `README.md` describes the contents, prerequisites, expected duration and
  size, and how to build, load, and check the result.

Use fresh build state and leave agents and services signed out. Run the
reusable [guest credential scan](lib/scan-secrets.sh) as root before saving.
Keep disk images and exports local. Commit the build recipe and inputs needed
to reproduce the installed environment. Downloads and package updates can
change the exact disk bytes between builds.

## Available images

- [base-ubuntu](base-ubuntu/README.md) is the signed-out Ubuntu profile cache.
- [homebox-smoke](homebox-smoke/README.md) contains the desktop layer and the
  development tools exercised by the guest smoke suite.
