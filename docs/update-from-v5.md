# Updating from version 5

[Rdio Scanner](https://github.com/chuot/rdio-scanner) has its server component completely rewritten in [GO](https://go.dev). Therefore, it is no longer possible to upgrade to version 6 as with previous versions.

You need to download the latest Rdio Scanner release from the [releases tab](https://github.com/chuot/rdio-scanner/releases) on the [main page](https://github.com/chuot/rdio-scanner).

> REMEMBER TO ALWAYS BACKUP YOUR DATABASE BEFORE ATTEMPTING AN UPDATE.

## Update steps for the standard sqlite database

- Stop your version 5 instance.
- Copy your old version 5 `database.sqlite` to a new folder where version 6 will be installed.
- Rename the database copy to `rdio-scanner.db`, the new default name.
- Run the new version 6 executable to update the database.
- Keep reading the PDF document that comes with the version 6 to make your instance as a service.

## Update steps for mysql/mariadb database

- Stop your version 5 instance.
- Make a backup of your MySQL/MariaDB database.
- Start the new version 6 executable with the proper -db_* arguments, see the `-h` output for more details.
- Keep reading the PDF document that comes with the version 6 to make your instance as a service.

## What if it is too late

Revert back to the latest [version 5.2.9](https://github.com/chuot/rdio-scanner/tree/3f2b184558e82317a010bd667ac3972f30998b1c) with the following commands:

- cd .../rdio-scanner
- git checkout 3f2b184
- cd client
- npm ci
- npm run build
- cd ../server
- npm ci
- restart your Rdio Scanner instance as usual
