# Sequelize

## Definition

**Rdio Scanner** utilize [Sequelize](https://sequelize.org/) for its *ORM*.

You can define specific 

```json
{
    "sequelize": {
        // One of *sequelize* dialects
        // Default is "sqlite"
        "dialect": "sqlite",

        // The host of your database server
        // Default is undefined
        "host": "localhost",

        // The name of the database on the database server
        // Default is undefined
        "name": "rdio_scanner",

        // The password to access your database server
        // Default is undefined
        "password": "password",

        // The host of your database server
        // Default is undefined
        "port": 3306,

        // The username to access your database server
        // Default is undefined
        "user": "rdio_scanner",

        // Only for sqlite, which is recommended
        // Default is "database.sqlite"
        "storage": "database.sqlite",
    }
}
```

> It is recommended to give a try to *sqlite* which has very good performances.
