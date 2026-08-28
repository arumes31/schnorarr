# Security policy

Please report suspected vulnerabilities privately through GitHub's **Report a vulnerability** feature. Do not open a public issue for an unpatched vulnerability.

The `main` branch and the latest published container image receive security fixes. Reports should include the affected version, deployment mode, reproduction steps, impact, and any suggested mitigation.

Deployment credentials and certificates are operator-managed. Rotate `INTERNAL_API_TOKEN`, `RSYNC_PASSWORD`, and `ADMIN_PASS` after suspected disclosure, and keep the receiver API and rsync port on a trusted private network.
