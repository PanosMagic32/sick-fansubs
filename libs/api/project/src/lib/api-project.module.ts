import { Module } from '@nestjs/common';
import { MongooseModule } from '@nestjs/mongoose';

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
  imports: [ProjectFeature],
  controllers: [ProjectController],
  providers: [ProjectService],
  exports: [ProjectFeature, ProjectService],
})
export class ApiProjectModule {}
