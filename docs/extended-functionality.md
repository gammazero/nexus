# Nexus Extended Functionality

## Extended Subscriber Allow/Deny Listing

Nexus extends WAMP's subscriber allow/deny listing functionality to support filtering on any attribute in the subscriber session details.

WAMP denies `authid` and `authrole` using `exclude_authid` and `exclude_authrole`, and allows these attributes using `eligible_authid` and `eligible_authrole`.
Nexus recognizes the publish options `exclude_xxx` and `eligible_xxx`, accompanied with a list of string values to match against, where `xxx` is the name of any attribute in the session details.

As an example, to allow sessions with `org_id=ycorp` or `org_id=zcorp`, a PUBLISH message specifies the following option:
```
eligible_org_id: ["ycorp", "zcorp"]
```

Note: Nexus includes all attributes from the HELLO message in the session details.

## Extended Session Meta API

Nexus extends the session meta API to provide additional information.

Normally, the `wamp.session.on_join` meta event message and the response to a `wamp.session.get` meta procedure includes the session details specified by the WAMP specification (`session`, `authid`, `authrole`, `authmethod`, `authprovider`, `transport`).
Nexus includes these details, and by default, includes **_all_** session details, including values the caller supplied in the HELLO message.
This allows clients to provide more information about themselves, via HELLO, that may then be used by other sessions to make decisions about who to send messages to.

There is an option to be more strict about what gets put into session meta details, should you want to limit what details are exposed by the session meta API.
The `MetaStrict` flag in the [`RealmConfig`](https://pkg.go.dev/github.com/gammazero/nexus/v3/router#RealmConfig) allows the router operator to restrict the session details provided by meta API.
