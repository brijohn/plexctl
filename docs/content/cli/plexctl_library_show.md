## plexctl library show

Show items in a library

```
plexctl library show [library_id] [flags]
```

### Options

```
      --all         Return all items (overrides count/page)
      --count int   Number of items to return per page (default 50)
  -h, --help        help for show
      --page int    Page number to return (default 1)
```

### Options inherited from parent commands

```
      --config string   config file (default is $HOME/.plexctl.yaml)
      --no-cache        Disable caching
  -o, --output string   Output format (table, json, json-pretty, yaml, csv, txt) (default "table")
      --sort string     column to sort by
  -v, --verbose count   increase verbosity
```

### SEE ALSO

* [plexctl library](plexctl_library.md)	 - Manage libraries

