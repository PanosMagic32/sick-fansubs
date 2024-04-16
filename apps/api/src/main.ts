import { Logger, ValidationPipe } from '@nestjs/common';
import { CorsOptions } from '@nestjs/common/interfaces/external/cors-options.interface';
import { NestFactory } from '@nestjs/core';
import { DocumentBuilder, SwaggerModule } from '@nestjs/swagger';

import { AppModule } from './app/app.module';
import { environment } from './environments/environment';

async function bootstrap() {
  const app = await NestFactory.create(AppModule);
  const globalPrefix = 'api';

  app.setGlobalPrefix(globalPrefix);
  app.useGlobalPipes(new ValidationPipe());

  let corsOptions: CorsOptions = {};

  if (environment.production) {
    corsOptions = {
      origin: 'http://localhost,https://sickfansubs.com,https://www.sickfansubs.com',
      allowedHeaders: 'Origin,X-Requested-With,Content-Type,Accept,Authorization',
      methods: 'GET,HEAD,PUT,PATCH,POST,DELETE,OPTIONS',
    };
  } else {
    corsOptions = {
      origin: '*',
      allowedHeaders: 'Origin,X-Requested-With,Content-Type,Accept,Authorization',
      methods: 'GET,HEAD,PUT,PATCH,POST,DELETE,OPTIONS',
    };

    const swaggerOptions = new DocumentBuilder()
      .setTitle('Sick-Fansubs API')
      .setDescription('Sick-Fansubs ReST API documentation')
      .setVersion(process.env.APP_VERSION)
      .build();

    const swaggerDocument = SwaggerModule.createDocument(app, swaggerOptions);

    SwaggerModule.setup('api-docs', app, swaggerDocument);
  }

  app.enableCors(corsOptions);

  const port = process.env.PORT || 3333;

  await app.listen(port);
  Logger.log(`🚀 Application is running on: http://localhost:${port}/${globalPrefix}`);
}

bootstrap();
