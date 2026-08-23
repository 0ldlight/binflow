import React from 'react';
import ComponentCreator from '@docusaurus/ComponentCreator';

export default [
  {
    path: '/binflow/docs/search',
    component: ComponentCreator('/binflow/docs/search', '1ac'),
    exact: true
  },
  {
    path: '/binflow/docs/',
    component: ComponentCreator('/binflow/docs/', '696'),
    routes: [
      {
        path: '/binflow/docs/',
        component: ComponentCreator('/binflow/docs/', 'c8e'),
        routes: [
          {
            path: '/binflow/docs/',
            component: ComponentCreator('/binflow/docs/', '56e'),
            routes: [
              {
                path: '/binflow/docs/admin',
                component: ComponentCreator('/binflow/docs/admin', 'd45'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/backup-restore',
                component: ComponentCreator('/binflow/docs/admin/backup-restore', '7aa'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/governance',
                component: ComponentCreator('/binflow/docs/admin/governance', '33f'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/groups-permissions',
                component: ComponentCreator('/binflow/docs/admin/groups-permissions', '556'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/rbac-roles',
                component: ComponentCreator('/binflow/docs/admin/rbac-roles', '430'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/real-env-appendix',
                component: ComponentCreator('/binflow/docs/admin/real-env-appendix', 'dc5'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/remote-virtual',
                component: ComponentCreator('/binflow/docs/admin/remote-virtual', '925'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/admin/token-step-up',
                component: ComponentCreator('/binflow/docs/admin/token-step-up', '782'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/api-reference',
                component: ComponentCreator('/binflow/docs/api-reference', 'f00'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/console',
                component: ComponentCreator('/binflow/docs/console', 'ebc'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/docker-registry',
                component: ComponentCreator('/binflow/docs/docker-registry', '5da'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/faq',
                component: ComponentCreator('/binflow/docs/faq', '601'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/guides',
                component: ComponentCreator('/binflow/docs/guides', '12d'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/guides/bf-cli',
                component: ComponentCreator('/binflow/docs/guides/bf-cli', '47d'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/guides/ldap-config',
                component: ComponentCreator('/binflow/docs/guides/ldap-config', '3ed'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/guides/migrate-artifactory',
                component: ComponentCreator('/binflow/docs/guides/migrate-artifactory', '0c9'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/guides/oidc-config',
                component: ComponentCreator('/binflow/docs/guides/oidc-config', 'db9'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/guides/s3-config',
                component: ComponentCreator('/binflow/docs/guides/s3-config', '6f6'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/install/binary',
                component: ComponentCreator('/binflow/docs/install/binary', 'fed'),
                exact: true
              },
              {
                path: '/binflow/docs/install/compose',
                component: ComponentCreator('/binflow/docs/install/compose', '680'),
                exact: true
              },
              {
                path: '/binflow/docs/install/docker',
                component: ComponentCreator('/binflow/docs/install/docker', '628'),
                exact: true
              },
              {
                path: '/binflow/docs/install/helm',
                component: ComponentCreator('/binflow/docs/install/helm', '4dc'),
                exact: true
              },
              {
                path: '/binflow/docs/install/k8s',
                component: ComponentCreator('/binflow/docs/install/k8s', '548'),
                exact: true
              },
              {
                path: '/binflow/docs/install/offline',
                component: ComponentCreator('/binflow/docs/install/offline', '326'),
                exact: true
              },
              {
                path: '/binflow/docs/install/systemd',
                component: ComponentCreator('/binflow/docs/install/systemd', '7f6'),
                exact: true
              },
              {
                path: '/binflow/docs/install/upgrade',
                component: ComponentCreator('/binflow/docs/install/upgrade', '2ce'),
                exact: true
              },
              {
                path: '/binflow/docs/integrations',
                component: ComponentCreator('/binflow/docs/integrations', '012'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/integrations/generic',
                component: ComponentCreator('/binflow/docs/integrations/generic', '011'),
                exact: true
              },
              {
                path: '/binflow/docs/integrations/maven',
                component: ComponentCreator('/binflow/docs/integrations/maven', '0be'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/integrations/npm',
                component: ComponentCreator('/binflow/docs/integrations/npm', 'a61'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/integrations/pypi',
                component: ComponentCreator('/binflow/docs/integrations/pypi', '381'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/metrics/prometheus-reference',
                component: ComponentCreator('/binflow/docs/metrics/prometheus-reference', '2b8'),
                exact: true,
                sidebar: "main"
              },
              {
                path: '/binflow/docs/',
                component: ComponentCreator('/binflow/docs/', 'dfe'),
                exact: true,
                sidebar: "main"
              }
            ]
          }
        ]
      }
    ]
  },
  {
    path: '*',
    component: ComponentCreator('*'),
  },
];
