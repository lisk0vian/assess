// crypto.randomUUID exists only in secure contexts, and classroom servers run
// plain HTTP on a LAN address. Local UI ids only need to be unique.
let n = 0;
export const uid = () => `${Date.now().toString(36)}-${(++n).toString(36)}`;
