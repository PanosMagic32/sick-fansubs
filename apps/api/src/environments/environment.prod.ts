export const environment = {
  production: true,
  corsOptions: {
    origin: ['http://localhost', 'https://sickfansubs.com', 'https://www.sickfansubs.com'],
    allowedHeaders: 'Origin,X-Requested-With,Content-Type,Accept,Authorization',
    methods: 'GET,HEAD,PUT,PATCH,POST,DELETE,OPTIONS',
  },
};
