package authpkg

// SignInSteps are the sign-in steps a generated application's server asks the login page
// for, as the server's pending status names them: the pending reason, and for the second
// factor the factor the policy requires after a colon. "confirmation" is the session
// library's password confirmation, which holds a directory identity that matched a user
// with a password; "mfa:email-code" is the mailed code requireSecondFactor names in every
// auth package (authTemplate), the library's own factor. The login page,
// @cccteam/resource-angular, declares the steps it renders under cccteam.signInSteps in its
// package.json, and the repository's CI holds this list to the declaration of the version
// the skeletons pin (.github/scripts/sign-in-steps.sh), so a release never pairs a server
// with a page that cannot render what the server asks: impulse 0.5.26 did, and every
// password sign-in dead-ended in the browser until 0.5.27. sign-in-steps.json beside this
// file is the list as the script reads it; TestSignInSteps holds the file to the list and
// the list to the policy the template writes.
var SignInSteps = []string{"confirmation", "mfa:email-code"}
