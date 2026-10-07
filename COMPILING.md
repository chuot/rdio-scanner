# How to compile

> RDIO SCANNER REPRESENTS A LOT OF INVESTMENT IN TIME, SUPPORT, TESTS AND MATERIALS. BY INTENTIONALLY MODIFYING ITS CODE TO REMOVE ITS IDENTITY, YOU HARM THE OPEN SOURCE PROJECT AND ITS AUTHOR. PLEASE BE RESPECTFUL, SPONSOR THIS PROJECT, USE NATIVE APPS IF POSSIBLE.

The official version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is compiled on a PC using the current version of Fedora Workstation. You should have no problem building on another platform as long as the prerequisites are available and installed.

## Install the prerequisites

Your os distribution may have all of the following prerequisites available in its own package repository. Make sure, however, that they are at the latest version. This is especially not the case with Ubuntu and its Node.js.

- latest version of git ([here](https://git-scm.com/downloads))
- latest version of gnu make ([here](https://www.gnu.org/software/make/))
- latest long-term support version of Node.js ([here](https://nodejs.org/en/))
- latest version of Go ([here](https://go.dev/dl/))
- latest version of Pandoc with pandoc-pdf ([here](https://pandoc.org/installing.html))
- latest version of TeX Live ([here](https://www.tug.org/texlive/))
- latest version of Info-Zip ([here](http://infozip.sourceforge.net/))
- latest version of podman ([here](https://podman.io/)), only for building containers

## Compile the app

Clone the official repository on your computer and start the build process.

        git clone https://github.com/chuot/rdio-scanner.git
        cd rdio-scanner
        make

The build compiles the two web applications before it compiles the server. The listener application and the administration console are two applications, in `internal/presentation/clients/scanner` and `internal/presentation/clients/admin`, and the modules they both use are in `internal/presentation/clients/shared`. Each application is built into the bundle directory of the Go package that embeds it, so `internal/presentation/serving/scanner/bundle` and `internal/presentation/serving/admin/bundle` are build output and hold nothing else.

        make client      # both applications
        make scanner     # the listener application only
        make admin       # the administration console only

The server binary carries both bundles. One executable serves the listener application at `/` and the administration console at `/admin/`.

When finished, you will find the precompiled versions for various platforms in the `dist` folder.

        rdio-scanner-freebsd-amd64-v7.0.0.zip
        rdio-scanner-linux-386-v7.0.0.zip
        rdio-scanner-linux-amd64-v7.0.0.zip
        rdio-scanner-linux-arm64-v7.0.0.zip
        rdio-scanner-linux-arm-v7.0.0.zip
        rdio-scanner-macos-amd64-v7.0.0.zip
        rdio-scanner-macos-arm64-v7.0.0.zip
        rdio-scanner-windows-amd64-v7.0.0.zip

**Happy Rdio scanning !**