// APP_VERSION is the release this build was made from. The build defines it (the define
// option of angular.json, 'dev' unless the workspace's build script is given VERSION, as
// the image's browser stage is), and the application hands it to the resource client as
// API_VERSION, so every request carries it in X-Api-Version and the server can refuse a
// build it no longer answers; a dev build sends no header.
declare const APP_VERSION: string;
