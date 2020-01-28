# Grafana Permission Sync

This tool assigns roles to users in Grafana, based on what Google groups they are in.
The mapping from Google groups to roles is managed by "rules" in the config file.

### Example
```
rules: [
    {
        # Everyone in the technology group should be able to view the two grafana organizations
        groups: [technology@my-company.com],
        orgs: ["Main Grafana Org", "Testing"],
        role: Viewer,
    },
    {
        # Also assign the Admin role to certain users 
        users: [ admin@my-company.com ],
        orgs: ["Main Grafana Org", "Testing"],
        role: Admin,
    },
] 
```

### 
