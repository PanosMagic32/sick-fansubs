import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

import { ApiMediaModule } from '@api/media';

import { ProjectController } from './project.controller';
import { ProjectService } from './project.service';
import { Project, ProjectSchema } from './schemas/project.schema';

const ProjectFeature = MongooseModule.forFeatureAsync([
  {
    name: Project.name,
    useFactory: () => {
      return ProjectSchema;
    },
  },
]);

@Module({
  imports: [ProjectFeature, ApiMediaModule],
  controllers: [ProjectController],
  providers: [ProjectService],
  exports: [ProjectFeature, ProjectService],
})
export class ApiProjectModule {}
