# Grafana Permission Sync
[![Docker Repository on Quay](https://quay.io/repository/google-cloud-tools/grafana-permission-sync/status "Docker Repository on Quay")](https://quay.io/repository/google-cloud-tools/grafana-permission-sync)
[![license](https://img.shields.io/github/license/google-cloud-tools/grafana-gsuite-sync.svg?maxAge=604800)](https://github.com/google-cloud-tools/grafana-gsuite-sync)


### What does it do?
This tool assigns roles to users in Grafana - based on what Google Groups they are in.
The mapping of what group results in what org/role is managed through [the config file](#config).

This tool is a full rewrite of (rewrite of [grafana-gsuite-sync](https://github.com/cloudworkz/grafana-gsuite-sync))


### How does it work?
1. Get all orgs and all users from grafana
2. Fetch all relevant google groups (once every `settings.groupsFetchInterval`)
3. For each user, compute what orgs they should be in and what role they should have. This "update plan" (the list of changes to be made) that will be printed to stdout, for example:
  ```json
  {"level":"info", "msg":"Promote user", "user":"Alice@COMPANY.com", "org":"Some Org Name [INT]", "oldRole":"Viewer", "role":"Admin"}`
  {"level":"info", "msg":"Remove user from org", "user":"Alice@COMPANY.com", "org":"Controlling"}
  {"level":"info", "msg":"Demote user", "user":"Alice@COMPANY.com", "org":"Some Org Name [PRD]", "oldRole":"Admin", "role":"Viewer"}
  {"level":"info", "msg":"Add user to org", "user":"SomeOtherUser", "org":"Some Org Name [PRD]", "role":"Viewer"}
```
4. Apply the changes slowly (capped at 10 operations per second)
5. Wait for `settings.applyInterval`, then repeat


### Requirements
- **Docker Image**: `docker pull quay.io/google-cloud-tools/grafana-permission-sync`
- **Google Service Account**: credentials (in `.json`) of a Google service account, which has permissions to impersonate a user that can see all groups ([instructions on how to set that up](https://developers.google.com/admin-sdk/directory/v1/guides/delegation#delegate_domain-wide_authority_to_your_service_account)). 
- Note: If credentials are omitted we fall back to [Application Default Credentials](https://cloud.google.com/docs/authentication/production). This can be useful for running in GKE for instance. 
- **Grafana Admin**: credentails of a Grafana user that has 'server admin' set
- **Config**: use [the example](https://github.com/cloudworkz/grafana-permission-sync/blob/master/demoConfig.yaml) as a starting point and add your rules


### Config
- By default the config file is loaded from `./config.yaml`,
  but you can override the path using the configPath flag: `--configPath=some/other/path/config.yaml`

Take a look at the [**the demo config file**](https://github.com/cloudworkz/grafana-permission-sync/blob/master/demoConfig.yaml) to see all settings

- The config supports hot reloading. When the file changes, it will be automatically reloaded.
  When a new config is loaded successfully (no parsing or validation errors), it will be applied (actually used) from the next iteration onwards. That basically just means a new config won't be applied in the middle of a running permission update.

- Hot reloading does currently only affect changes to the `settings:` and `rules:` blocks. Other blocks (`google:`, `grafana:`) are not updated and require the application to be restarted. (Will be fixed soon)


### Rules
- The `orgs: ` property supports regex, but only if the element is enclosed in `//`!
    That means `orgs: [ ".*" ]` will not work, it will not be interpreted as a regex!
    For example: to match everything you'd write `orgs: [ /.*/ ]` or with quotes `orgs: [ "/.*/" ]` (because regex can contain all sorts of symbols).

- The `note: ` property will be shown as the reason for each change

- The only required property in each rule is `role: ` 

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


### Why are there two different time intervals?
- `settings.groupsFetchInterval` controls how often google groups are fetched.
  To avoid hitting googles rate limit, you probably want this to have a pretty high value (30 minutes or so).

- `settings.applyInterval` controls how often the main loop runs.
  Grafana creates an account for a user when they login for the first time.
  When the new user account is created, grafana-permission-sync can assign the correct
  permissions (organization membership and roles) the next time it computes an update.
  So we want to do this pretty often (scanning for newly created users and assigning the right permissions to them).

### Health/Liveness

Kubernetes ready and liveness probes: `/admin/ready` and `/admin/alive`
