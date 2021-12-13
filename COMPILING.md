# How to compile

The official version of [Rdio Scanner](https://github.com/chuot/rdio-scanner) is compiled on a PC using the current version of Fedora Workstation. You should have no problem building on another platform as long as the prerequisites are available and installed.

## Install the prerequisites

Your os distribution may have all of the following prerequisites available in its own package repository. Make sure, however, that they are at the latest version. This is especially not the case with Ubuntu and its Node.js.

- latest version of git ([here](https://git-scm.com/downloads))
- latest version of gnu make ([here](https://www.gnu.org/software/make/))
- latest long-term support version of Node.js ([here](https://nodejs.org/en/))
- latest version of Go ([here](https://go.dev/dl/))
- latest version of Pandoc with pandoc-pdf ([here](https://pandoc.org/installing.html))
- latest version of Info-Zip ([here](http://infozip.sourceforge.net/))
- latest version of podman ([here](https://podman.io/)), only for building containers

## Compile the app

Clone the official repository on your computer and start the build process.

        git clone https://github.com/chuot/rdio-scanner.git
        cd rdio-scanner
        make

When finished, you will find the precompiled versions for various platforms in the `dist` folder.

        rdio-scanner-darwin-amd64-v6.0.6.zip
        rdio-scanner-darwin-arm64-v6.0.6.zip
        rdio-scanner-freebsd-amd64-v6.0.6.zip
        rdio-scanner-linux-386-v6.0.6.zip
        rdio-scanner-linux-amd64-v6.0.6.zip
        rdio-scanner-linux-arm64-v6.0.6.zip
        rdio-scanner-linux-arm-v6.0.6.zip
        rdio-scanner-windows-amd64-v6.0.6.zip

**Happy Rdio scanning !**