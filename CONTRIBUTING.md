## Welcome
Thank you for your interest in contributing to the nexus project.  Any improvements to the core code, documentation, test code, or external utilities, to help nexus be the best in class WAMP implementation are welcome.

Please take a moment to review this document in order to make the contribution process easy and effective for everyone involved.

## Contributor Responsibilities

- Use the GitHub Issue tracker to let us know what your are planning, so we can provide feedback.
- Provide tests and documentation whenever possible. New features or functionality will almost alway require proper testing and documentation before they are accepted. If contributing a bug fix, provide a failing test case that your change solves.
- Agree to the [contributor agreement](#contributor-agreement)
- Open a GitHub Pull Request with your changes and we will review your contribution and respond as quickly as possible. Keep in mind that this is an open source project, and it may take us some time to respond. Your patience is appreciated.

## Contributor Agreement

By submitting a pull request, or other form of submission of a contribution to this project, you agree:

1. To license your contribution under the MIT license, to this project, perpetually and irrevocably. 
2. Your controbution does not infringe on anyone elses copyright, patent calims, or any grant of rights which you have made to third parties.
3. You acknowledge that the nexus project is not obligated to use your contribution and may decide to include any contribution nexus considers appropriate.
4. You have legal authority to enter into this agreement.

## Code Review Process

The core team looks at Pull Requests on a regular basis.  If your change is not an obvious fix, we will discuss the PR in comments on the PR, in GitHub issues, or hold a meeting hold in a public Google Hangout. After feedback has been given we expect responses within two weeks. After two weeks we may close the pull request if it isn't showing any activity.

Once the core team has decided to accept a PR, one of the maintainers will merge it.  Minor obvious changes, like spelling or grammar fixes or fixes to other obvious minor errors may me mered immediately by a maintainer.

Please make sure your PR meets the guidelines in the [PR Review Checklist](#pull-request-review-checklist)

## Help

**Working on your first Pull Request?** You can learn how from this *free* series [How to Contribute to an Open Source Project on GitHub](https://egghead.io/series/how-to-contribute-to-an-open-source-project-on-github)

At this point, you're ready to make your changes! Feel free to ask for help.  If you want to know know where to put contributins, how to test, or just discuss changes you think would improve the project, then please ask; we are happy that you are interested.

## Pull Request Review Checklist

### General

- [ ] Is this change useful to the project in general and will benefit others?
- [ ] Check for overlap with other PRs.
- [ ] Are there long-term implications of the change; How will it affect existing projects that are dependent on this? 
- [ ] Is there any way the change could be implemented as a separate package, for better modularity and flexibility?
- [ ] Are your changes as simple as possible, and can you justify all your implementation choices?

## Check the Code

- [ ] If it does multiple things, break into separate smaller PRs if possible.
- [ ] Does it pass all existing unit tests and automated acceptance tests (AATs)?
- [ ] Did you include unit tests with as close to 100% coverage of your contribution as practical?
- [ ] Did you create new AATs for you code, or do existing AATs cover your changes?
- [ ] Did you comment you code, when it is not obvious, to explain the intent and why it does what it does? 
- [ ] Is the code consistent?  Did you use `go fmt` to format any Go code?
- [ ] Did you spell/grammar check documentation and comments?

Expect it to take the time to get things right. PRs almost always require additional improvements to meet the bar for quality. This usually takes several commits on top of the original PR.
