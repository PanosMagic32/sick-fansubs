import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiBlogPostModule } from '@sick/api/blog-post';
import { ApiUserModule } from '@sick/api/user';

const url = process.env.MONGO_URL || 'localhost';

@Module({
  imports: [ApiBlogPostModule, ApiUserModule, MongooseModule.forRoot(`mongodb://${url}:27017`)],
})
export class AppModule {}
