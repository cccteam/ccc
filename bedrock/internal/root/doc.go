// Package root is the root organization's engine: the one organization made by hand, by
// bedrock org bootstrap, and kept true by its triggers' runs with no terraform. It holds
// what the placement says the organization is (desired), reads what Google Cloud and
// GitHub hold, and makes the difference true in Apply mode or reports it in Check and
// Plan; the bootstrap, the manifest flow that makes the GitHub Apps, the triggers' runs
// and the destroy are built on it.
package root
