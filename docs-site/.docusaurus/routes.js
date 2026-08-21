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
    component: ComponentCreator('/binflow/docs/', 'd97'),
    routes: [
      {
        path: '/binflow/docs/',
        component: ComponentCreator('/binflow/docs/', '5da'),
        routes: [
          {
            path: '/binflow/docs/',
            component: ComponentCreator('/binflow/docs/', '3bb'),
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
                path: '/binflow/docs/admin/remote-virtual',
                component: ComponentCreator('/binflow/docs/admin/remote-virtual', '925'),
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
                path: '/binflow/docs/integrations',
                component: ComponentCreator('/binflow/docs/integrations', '012'),
                exact: true,
                sidebar: "main"
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
