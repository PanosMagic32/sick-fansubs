export const environment = {
  production: false,
  corsOptions: {
    origin: ['http://localhost:4200', 'http://127.0.0.1:4200'],
    credentials: true,
    allowedHeaders: 'Origin,X-Requested-With,Content-Type,Accept,Authorization',
    methods: 'GET,HEAD,PUT,PATCH,POST,DELETE,OPTIONS',
  },
};
