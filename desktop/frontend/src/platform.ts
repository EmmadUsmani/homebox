export const isMac = typeof navigator !== "undefined" && /Macintosh|Mac OS X/.test(navigator.userAgent);
export const thisHost = isMac ? "this Mac" : "this host";
