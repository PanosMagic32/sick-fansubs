import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiBlogPostModule } from '@sick/api/blog-post';

const url = process.env.MONGO_URL || 'localhost';

@Module({
  imports: [ApiBlogPostModule, MongooseModule.forRoot(`mongodb://${url}:27017`)],
})
export class AppModule {}
