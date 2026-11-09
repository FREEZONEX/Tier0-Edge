import { randomBytes } from 'node:crypto';

// rN is a developer-selected business revision, never a build counter.
export function validateProductVersion(version) {
  if (!/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-r[1-9]\d*)?$/.test(version)) {
    throw Error(`Invalid PRODUCT_VERSION: ${version}; use X.Y.Z or X.Y.Z-rN (N >= 1)`);
  }
  return version;
}

export function packageVersion(product, existing = '') {
  validateProductVersion(product);
  if (existing && (!existing.startsWith(product + '-') || !/^[0-9a-f]{6}$/.test(existing.slice(product.length + 1)))) {
    throw Error(`PACKAGE_VERSION must be ${product}-<6 lowercase hex characters>`);
  }
  return existing || `${product}-${randomBytes(3).toString('hex')}`;
}
