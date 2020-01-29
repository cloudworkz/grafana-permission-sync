# Grafana Permission Sync

This tool assigns roles to users in Grafana, based on what Google groups they are in.
The mapping from Google groups to roles is managed by "rules" in the config file.

### Setup TODO

```
docker pull quay.io...
```

### Config

- By default the config file is loaded from `./config.yaml`, but you can override the path using the configPath flag: `--configPath=some/other/path/config.yaml`

To see all the settings, take a look at [the demo config file](https://github.com/cloudworkz/grafana-permission-sync/blob/master/demoConfig.yaml)

### Rules

Example:
```yaml
rules: [
    {
        # Everyone in the technology group should be able to view the two grafana organizations
        note: "tech viewers", # used to show in the reason field
        groups: [technology@my-company.com],
        orgs: ["Main Grafana Org", "Testing"],
        role: Viewer,
    },
    {
        # Also assign the Admin role to certain users 
        note: "global admins", 
        users: [ admin@my-company.com ], # individual users
        orgs: ["/.*/"],
        role: Admin,
    },
] 
```

- The `orgs: ` property supports regex, but only if the element is enclosed in `//`!
  That means `orgs: [ ".*" ]` will not work, it will not be interpreted as a regex!
  For example: to match everything you'd write `orgs: [ /.*/ ]` or with quotes `orgs: [ "/.*/" ]` (because regex can contain all sorts of symbols).

- The `note: ` property will be shown as the reason for each change